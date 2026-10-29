package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"cpa-quota/internal/model"
)

const (
	claudeUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	claudeGrantURL   = "https://api.anthropic.com/api/oauth/usage?cedar_ember=1&skip_spend=1"
	claudeProfileURL = "https://api.anthropic.com/api/oauth/profile"
	claudeClaimBase  = "https://api.anthropic.com/api/organizations"
)

var (
	claudeOrganizationIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	claudeGrantIDPattern        = regexp.MustCompile(`^[a-z0-9_-]{1,40}$`)
	claudeRequestIDPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	errClaudeAuthentication     = errors.New("Claude authentication failed")
	errClaudeRateLimited        = errors.New("rate limited by Anthropic")
)

var claudeWindows = []struct {
	key   string
	label string
}{
	{"five_hour", "5 hour"},
	{"seven_day", "Weekly"},
	{"seven_day_opus", "Weekly Opus"},
	{"seven_day_sonnet", "Weekly Sonnet"},
	{"seven_day_oauth_apps", "Weekly OAuth apps"},
	{"seven_day_cowork", "Weekly Cowork"},
	{"iguana_necktie", "Weekly Fable"},
}

type claudeProvider struct{}

func init() {
	register([]string{"claude", "anthropic"}, claudeProvider{})
}

func (claudeProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "token"),
		ctx.Attributes["api_key"],
	)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("claude credential is missing an access token")
	}

	usageResp, errUsage := ctx.HTTP.Do(http.MethodGet, claudeUsageURL, claudeHeaders(token), nil)
	if errUsage != nil {
		return model.QuotaFetchResponse{}, errUsage
	}
	usage, errDecode := decodeBody(usageResp)
	if errDecode != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("claude usage: %w", errDecode)
	}

	profile, _ := fetchClaudeProfile(ctx, token)
	response := model.QuotaFetchResponse{
		Subscription: claudeSubscription(profile),
		Groups:       []model.QuotaGroup{{DisplayName: "Claude"}},
	}
	group := &response.Groups[0]

	for _, window := range claudeWindows {
		// Fable moved to the newer limits[] block; the legacy
		// iguana_necktie window is handled separately below.
		if window.key == "iguana_necktie" {
			continue
		}
		record, ok := usage[window.key].(map[string]any)
		if !ok {
			continue
		}
		percent, okPercent := mapNumber(record["utilization"])
		if !okPercent {
			continue
		}
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            window.key,
			RemainingFraction: remainingFromUsedPercent(percent),
			ResetTime:         isoFromAny(record["resets_at"]),
		})
	}
	appendClaudeFableBuckets(group, usage)

	if extra, ok := usage["extra_usage"].(map[string]any); ok && extra["is_enabled"] == true {
		used, _ := mapNumber(extra["used_credits"])
		limit, _ := mapNumber(extra["monthly_limit"])
		percent, okPercent := mapNumber(extra["utilization"])
		if !okPercent && limit > 0 {
			percent = used / limit * 100
		}
		description := ""
		if limit > 0 {
			description = fmt.Sprintf("$%.2f / $%.2f", used/100, limit/100)
		}
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            "extra_usage",
			RemainingFraction: remainingFromUsedPercent(percent),
			Description:       description,
		})
	}

	// Organization and prepaid dollar balances use their own window keys.
	for key, value := range usage {
		if isClaudeWindow(key) || key == "extra_usage" || key == "cedar_ember" {
			continue
		}
		record, ok := value.(map[string]any)
		if !ok || !claudeDollarWindow(record) {
			continue
		}
		appendClaudeDollarBucket(group, key, record)
	}

	if grants, err := fetchClaudeGrantStatus(ctx, token); err == nil {
		total := 0.0
		for _, grant := range grants.Grants {
			total += grant.ResetsLeft
		}
		if grants.Eligible {
			response.Summary = append(response.Summary, model.QuotaMetric{
				Key:   "reset_grants",
				Label: "Reset grants",
				Value: total,
				Unit:  "times",
			})
		}
	}

	return response, nil
}

// appendClaudeFableBuckets resolves the Fable weekly limit and the legacy
// iguana_necktie window. Newer usage payloads expose Fable through limits[]
// with kind weekly_scoped; iguana_necktie can also be a dollar-denominated
// cloud session credit pool instead of a percentage window.
func appendClaudeFableBuckets(group *model.QuotaGroup, usage map[string]any) {
	iguana, hasIguana := usage["iguana_necktie"].(map[string]any)
	if hasIguana && claudeDollarWindow(iguana) {
		appendClaudeDollarBucket(group, "cloud_session_credits", iguana)
		hasIguana = false
	}

	percent, reset, ok := claudeFableLimit(usage)
	if !ok && hasIguana {
		percent, ok = mapNumber(iguana["utilization"])
		reset = iguana["resets_at"]
	}
	if !ok {
		return
	}
	group.Buckets = append(group.Buckets, model.QuotaBucket{
		Window:            "seven_day_fable",
		RemainingFraction: remainingFromUsedPercent(percent),
		ResetTime:         isoFromAny(reset),
	})
}

// claudeFableLimit reads limits[] and prefers the active weekly_scoped Fable
// entry, falling back to the first matching entry.
func claudeFableLimit(usage map[string]any) (float64, any, bool) {
	var fallbackPercent float64
	var fallbackReset any
	hasFallback := false
	for _, raw := range getArray(usage, "limits") {
		record, ok := raw.(map[string]any)
		if !ok || !strings.EqualFold(asString(record["kind"]), "weekly_scoped") {
			continue
		}
		scope, _ := record["scope"].(map[string]any)
		model, _ := scope["model"].(map[string]any)
		name := strings.ToLower(strings.TrimSpace(asString(model["display_name"])))
		if name != "fable" && name != "fable 5" {
			continue
		}
		percent, okPercent := mapNumber(record["percent"])
		if !okPercent {
			continue
		}
		if record["is_active"] == true {
			return percent, record["resets_at"], true
		}
		if !hasFallback {
			fallbackPercent, fallbackReset, hasFallback = percent, record["resets_at"], true
		}
	}
	return fallbackPercent, fallbackReset, hasFallback
}

// claudeDollarWindow reports whether a window carries dollar balances instead
// of a utilization percentage.
func claudeDollarWindow(record map[string]any) bool {
	for _, key := range []string{"limit_dollars", "used_dollars", "remaining_dollars"} {
		if _, ok := mapNumber(record[key]); ok {
			return true
		}
	}
	return false
}

func appendClaudeDollarBucket(group *model.QuotaGroup, key string, record map[string]any) {
	limit, _ := mapNumber(record["limit_dollars"])
	used, _ := mapNumber(record["used_dollars"])
	remaining, hasRemaining := mapNumber(record["remaining_dollars"])
	percent, okPercent := mapNumber(record["utilization"])
	if !okPercent {
		switch {
		case limit > 0 && hasRemaining:
			percent = (limit - remaining) / limit * 100
		case limit > 0:
			percent = used / limit * 100
		}
	}
	description := ""
	switch {
	case limit > 0 && used > 0:
		description = fmt.Sprintf("$%.2f / $%.2f", used, limit)
	case limit > 0 && hasRemaining:
		description = fmt.Sprintf("$%.2f / $%.2f", limit-remaining, limit)
	case hasRemaining:
		description = fmt.Sprintf("$%.2f remaining", remaining)
	}
	group.Buckets = append(group.Buckets, model.QuotaBucket{
		Window:            key,
		RemainingFraction: remainingFromUsedPercent(percent),
		ResetTime:         isoFromAny(record["resets_at"]),
		Description:       description,
	})
}

func (claudeProvider) Reset(ctx *Context) (model.QuotaResetResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "token"),
		ctx.Attributes["api_key"],
	)
	if token == "" {
		return model.QuotaResetResponse{}, fmt.Errorf("claude credential is missing an access token")
	}
	status, errStatus := fetchClaudeGrantStatus(ctx, token)
	if errStatus != nil {
		switch {
		case errors.Is(errStatus, errClaudeAuthentication):
			return model.QuotaResetResponse{Success: false, Message: errClaudeAuthentication.Error()}, nil
		case errors.Is(errStatus, errClaudeRateLimited):
			return model.QuotaResetResponse{Success: false, Message: errClaudeRateLimited.Error()}, nil
		default:
			return model.QuotaResetResponse{}, fmt.Errorf("read Claude reset grants: %w", errStatus)
		}
	}
	grantID, reason := usableClaudeGrant(status, ctx.Now)
	if grantID == "" {
		return model.QuotaResetResponse{Success: false, Message: reason}, nil
	}
	if !claudeGrantIDPattern.MatchString(grantID) {
		return model.QuotaResetResponse{Success: false, Message: "Claude reset grant id is invalid"}, nil
	}

	profile, errProfile := fetchClaudeProfile(ctx, token)
	if errProfile != nil {
		switch {
		case errors.Is(errProfile, errClaudeAuthentication):
			return model.QuotaResetResponse{Success: false, Message: errClaudeAuthentication.Error()}, nil
		case errors.Is(errProfile, errClaudeRateLimited):
			return model.QuotaResetResponse{Success: false, Message: errClaudeRateLimited.Error()}, nil
		default:
			return model.QuotaResetResponse{}, errProfile
		}
	}
	organization := claudeOrganization(profile)
	if organization == "" {
		return model.QuotaResetResponse{Success: false, Message: "Claude organization id is unavailable"}, nil
	}
	key := "claude|" + ctx.AuthIndex + "|" + grantID
	requestID := resetRequestID(key)
	if !claudeRequestIDPattern.MatchString(requestID) {
		return model.QuotaResetResponse{Success: false, Message: "Claude reset request id is invalid"}, nil
	}
	body, _ := json.Marshal(map[string]string{
		"program":    "cedar_ember",
		"grant_id":   grantID,
		"request_id": requestID,
	})
	resp, errClaim := ctx.HTTP.Do(
		http.MethodPost,
		fmt.Sprintf("%s/%s/reset_rate_limits", claudeClaimBase, organization),
		claudeHeaders(token),
		body,
	)
	if errClaim != nil {
		return model.QuotaResetResponse{}, errClaim
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		clearResetRequest(key)
		return model.QuotaResetResponse{Success: false, Message: errClaudeRateLimited.Error()}, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		clearResetRequest(key)
		return model.QuotaResetResponse{Success: false, Message: errClaudeAuthentication.Error()}, nil
	}
	result := ""
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var decoded map[string]any
		if json.Unmarshal(resp.Body, &decoded) == nil {
			result = asString(decoded["result"])
		}
	}
	switch result {
	case "reset", "already_used":
		clearResetRequest(key)
		return model.QuotaResetResponse{Success: true, Message: result}, nil
	case "not_limited", "cooldown", "ineligible", "unavailable":
		clearResetRequest(key)
		return model.QuotaResetResponse{Success: false, Message: result}, nil
	case "":
		return model.QuotaResetResponse{Success: false, Message: fmt.Sprintf("Anthropic returned %d", resp.StatusCode)}, nil
	default:
		return model.QuotaResetResponse{Success: false, Message: "Anthropic returned an unknown result"}, nil
	}
}

func claudeHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization":  "Bearer " + token,
		"Content-Type":   "application/json",
		"anthropic-beta": "oauth-2025-04-20",
		"user-agent":     "claude-cli/2.1.280 (external, cli)",
	}
}

func fetchClaudeProfile(ctx *Context, token string) (map[string]any, error) {
	resp, err := ctx.HTTP.Do(http.MethodGet, claudeProfileURL, claudeHeaders(token), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errClaudeAuthentication
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, errClaudeRateLimited
	}
	return decodeBody(resp)
}

func claudeSubscription(profile map[string]any) *model.QuotaSubscription {
	if profile == nil {
		return nil
	}
	plan := ""
	if account, ok := profile["account"].(map[string]any); ok {
		if account["has_claude_max"] == true {
			plan = "Max"
		} else if account["has_claude_pro"] == true {
			plan = "Pro"
		}
	}
	if plan == "" {
		plan = lookupString(profile, "rate_limit_tier")
	}
	if plan == "" {
		return nil
	}
	return &model.QuotaSubscription{Plan: plan}
}

type claudeGrant struct {
	ID               string
	ResetsLeft       float64
	Paused           bool
	UsableNow        bool
	UseRequiresLimit bool
	StartsAt         time.Time
	EndsAt           time.Time
}

type claudeGrantStatus struct {
	Eligible      bool
	AtLimit       bool
	CooldownUntil time.Time
	NextGrantID   string
	Grants        []claudeGrant
}

func fetchClaudeGrantStatus(ctx *Context, token string) (claudeGrantStatus, error) {
	resp, err := ctx.HTTP.Do(http.MethodGet, claudeGrantURL, claudeHeaders(token), nil)
	if err != nil {
		return claudeGrantStatus{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return claudeGrantStatus{}, errClaudeAuthentication
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return claudeGrantStatus{}, errClaudeRateLimited
	}
	payload, errDecode := decodeBody(resp)
	if errDecode != nil {
		return claudeGrantStatus{}, errDecode
	}
	block, ok := payload["cedar_ember"].(map[string]any)
	if !ok {
		return claudeGrantStatus{}, fmt.Errorf("Claude reset grant data is unavailable")
	}
	status := claudeGrantStatus{
		Eligible:      block["eligible"] == true,
		AtLimit:       block["at_limit"] == true,
		CooldownUntil: parseTime(block["cooldown_until"]),
		NextGrantID:   asString(block["next_grant_id"]),
	}
	for _, raw := range getArray(block, "grants") {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		left, okLeft := mapNumber(record["resets_left"])
		if !okLeft {
			continue
		}
		grant := claudeGrant{
			ID:               asString(record["id"]),
			ResetsLeft:       left,
			Paused:           record["paused"] == true,
			UsableNow:        record["usable_now"] == true,
			UseRequiresLimit: record["use_requires_limit"] != false,
			StartsAt:         parseTime(record["starts_at"]),
			EndsAt:           parseTime(record["ends_at"]),
		}
		if grant.ID != "" {
			status.Grants = append(status.Grants, grant)
		}
	}
	return status, nil
}

func claudeOrganization(profile map[string]any) string {
	organization := getMap(profile, "organization")
	if organization == nil {
		return ""
	}
	uuid := strings.ToLower(asString(organization["uuid"]))
	if !claudeOrganizationIDPattern.MatchString(uuid) {
		return ""
	}
	return uuid
}

func usableClaudeGrant(status claudeGrantStatus, now time.Time) (string, string) {
	if !status.Eligible {
		return "", "the account is not eligible for Claude reset grants"
	}
	if !status.CooldownUntil.IsZero() && status.CooldownUntil.After(now) {
		return "", "Claude reset is still cooling down"
	}
	var fallback string
	for _, grant := range status.Grants {
		if grant.ResetsLeft <= 0 || grant.Paused || !grant.UsableNow {
			continue
		}
		if grant.UseRequiresLimit && !status.AtLimit {
			continue
		}
		if !grant.StartsAt.IsZero() && grant.StartsAt.After(now) {
			continue
		}
		if !grant.EndsAt.IsZero() && !grant.EndsAt.After(now) {
			continue
		}
		if grant.ID == status.NextGrantID {
			return grant.ID, ""
		}
		if fallback == "" || grant.ID < fallback {
			fallback = grant.ID
		}
	}
	if fallback == "" {
		return "", "no usable Claude reset grant"
	}
	return fallback, ""
}

func parseTime(value any) time.Time {
	text := isoFromAny(value)
	if text == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func isClaudeWindow(key string) bool {
	for _, window := range claudeWindows {
		if window.key == key {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Reset request ids are reused for ten minutes so an ambiguous retry cannot
// spend a second grant.
var (
	resetRequestMu sync.Mutex
	resetRequests  = map[string]struct {
		id string
		at time.Time
	}{}
)

func resetRequestID(key string) string {
	resetRequestMu.Lock()
	defer resetRequestMu.Unlock()
	if existing, ok := resetRequests[key]; ok && time.Since(existing.at) < 10*time.Minute {
		return existing.id
	}
	id := newRequestID()
	resetRequests[key] = struct {
		id string
		at time.Time
	}{id: id, at: time.Now()}
	return id
}

func clearResetRequest(key string) {
	resetRequestMu.Lock()
	defer resetRequestMu.Unlock()
	delete(resetRequests, key)
}
