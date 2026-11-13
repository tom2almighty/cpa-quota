package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"cpa-quota/internal/model"
)

var antigravityQuotaURLs = []string{
	"https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:retrieveUserQuotaSummary",
	"https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
}

// antigravityUserAgent mirrors the Antigravity Hub client UA: version, client
// family, platform metadata and release changelist. Keep it aligned with the Hub
// release CLIProxyAPI emulates (internal/misc/antigravity_version.go); Cloud Code
// rejects newer models for clients below 2.23.0.
const antigravityUserAgent = "antigravity/hub/2.29.0 (aidev_client; os_type=darwin; arch=arm64; cl=1013060670)"

type antigravityProvider struct{}

func init() {
	register([]string{"antigravity"}, antigravityProvider{})
}

func (antigravityProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "token"),
		ctx.Attributes["api_key"],
	)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("antigravity credential is missing an access token")
	}
	projectID := firstNonEmpty(
		lookupString(ctx.Auth, "project_id", "projectId"),
		lookupString(ctx.Metadata, "project_id", "projectId"),
	)
	if projectID == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("antigravity credential is missing a project id")
	}
	body, _ := json.Marshal(map[string]string{"project": projectID})
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
		"User-Agent":    antigravityUserAgent,
	}
	merged := newAntigravityMerger()
	hadSuccess := false
	var lastErr error
	for _, url := range antigravityQuotaURLs {
		resp, err := ctx.HTTP.Do(http.MethodPost, url, headers, body)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("antigravity quota returned %d: %s", resp.StatusCode, truncate(string(resp.Body), 200))
			// An invalid token fails on every host, so stop probing.
			if resp.StatusCode == http.StatusUnauthorized {
				break
			}
			continue
		}
		payload, errDecode := decodeBody(resp)
		if errDecode != nil {
			lastErr = errDecode
			continue
		}
		hadSuccess = true
		merged.merge(payload)
		// Daily hosts can return a successful but partial Claude/GPT-only
		// summary; keep probing until the Gemini pool is present.
		if merged.hasGemini() {
			break
		}
	}
	if !hadSuccess {
		if lastErr == nil {
			lastErr = fmt.Errorf("antigravity quota is unavailable")
		}
		return model.QuotaFetchResponse{}, lastErr
	}
	return parseAntigravityQuota(map[string]any{"groups": merged.payloadGroups()}), nil
}

// antigravityMerger accumulates quota groups across hosts. The first successful
// response is not necessarily complete, so groups and buckets are merged in
// arrival order while the first value for a bucket wins.
type antigravityMerger struct {
	groups []*antigravityMergedGroup
	index  map[string]int
	gemini bool
}

type antigravityMergedGroup struct {
	record map[string]any
	seen   map[string]struct{}
}

func newAntigravityMerger() *antigravityMerger {
	return &antigravityMerger{index: map[string]int{}}
}

func (m *antigravityMerger) merge(payload map[string]any) {
	for i, rawGroup := range getArray(payload, "groups") {
		record, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		label := firstNonEmpty(asString(record["displayName"]), asString(record["display_name"]))
		key := label
		if key == "" {
			key = fmt.Sprintf("quota-group-%d", i)
		}
		slot, exists := m.index[key]
		if !exists {
			group := &antigravityMergedGroup{
				record: map[string]any{"displayName": label, "buckets": []any{}},
				seen:   map[string]struct{}{},
			}
			if description := asString(record["description"]); description != "" {
				group.record["description"] = description
			}
			m.groups = append(m.groups, group)
			slot = len(m.groups) - 1
			m.index[key] = slot
		}
		group := m.groups[slot]
		buckets, _ := group.record["buckets"].([]any)
		for j, rawBucket := range getArray(record, "buckets") {
			bucket, ok := rawBucket.(map[string]any)
			if !ok {
				continue
			}
			bucketKey := antigravityBucketKey(bucket)
			if bucketKey == "" {
				bucketKey = fmt.Sprintf("bucket-%d", j)
			}
			if _, exists := group.seen[bucketKey]; exists {
				continue
			}
			group.seen[bucketKey] = struct{}{}
			buckets = append(buckets, bucket)
			if antigravityGeminiBucket(label, bucket) {
				m.gemini = true
			}
		}
		group.record["buckets"] = buckets
	}
}

func (m *antigravityMerger) payloadGroups() []any {
	groups := make([]any, 0, len(m.groups))
	for _, group := range m.groups {
		groups = append(groups, group.record)
	}
	return groups
}

func (m *antigravityMerger) hasGemini() bool { return m.gemini }

func antigravityBucketKey(raw any) string {
	bucket, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	if id := firstNonEmpty(asString(bucket["bucketId"]), asString(bucket["bucket_id"])); id != "" {
		return id
	}
	window := asString(bucket["window"])
	label := firstNonEmpty(asString(bucket["displayName"]), asString(bucket["display_name"]))
	if window == "" && label == "" {
		return ""
	}
	return window + "|" + label
}

func antigravityGeminiBucket(groupLabel string, bucket map[string]any) bool {
	id := strings.ToLower(firstNonEmpty(asString(bucket["bucketId"]), asString(bucket["bucket_id"])))
	if strings.HasPrefix(id, "gemini-") || strings.HasPrefix(id, "gemini_") {
		return true
	}
	if strings.Contains(strings.ToLower(groupLabel), "gemini") {
		return true
	}
	label := strings.ToLower(firstNonEmpty(asString(bucket["displayName"]), asString(bucket["display_name"])))
	return strings.Contains(label, "gemini")
}

func (antigravityProvider) Reset(*Context) (model.QuotaResetResponse, error) {
	return model.QuotaResetResponse{Success: false, Message: "Antigravity does not support quota reset"}, nil
}

func parseAntigravityQuota(payload map[string]any) model.QuotaFetchResponse {
	response := model.QuotaFetchResponse{}
	for _, rawGroup := range getArray(payload, "groups") {
		groupRecord, ok := rawGroup.(map[string]any)
		if !ok {
			continue
		}
		group := model.QuotaGroup{DisplayName: firstNonEmpty(asString(groupRecord["displayName"]), asString(groupRecord["display_name"]))}
		groupDescription := antigravityGroupDescription(group.DisplayName, asString(groupRecord["description"]))
		rawBuckets := getArray(groupRecord, "buckets")
		weeklyDepleted := false
		for _, rawBucket := range rawBuckets {
			bucketRecord, ok := rawBucket.(map[string]any)
			if !ok {
				continue
			}
			if antigravityWindowKey(asString(bucketRecord["displayName"]), asString(bucketRecord["window"])) == "weekly" {
				if remaining, okRemaining := mapNumber(bucketRecord["remainingFraction"]); okRemaining && remaining <= 0 {
					weeklyDepleted = true
				}
			}
		}
		for _, rawBucket := range rawBuckets {
			bucketRecord, ok := rawBucket.(map[string]any)
			if !ok {
				continue
			}
			label := firstNonEmpty(asString(bucketRecord["displayName"]), asString(bucketRecord["display_name"]))
			window := antigravityWindowKey(label, firstNonEmpty(asString(bucketRecord["window"])))
			remaining, okRemaining := mapNumber(bucketRecord["remainingFraction"])
			if !okRemaining {
				continue
			}
			description := firstNonEmpty(asString(bucketRecord["description"]), groupDescription)
			if weeklyDepleted && window == "five_hour" && remaining >= 1 {
				if description != "" {
					description += " · Weekly limit exhausted"
				} else {
					description = "Weekly limit exhausted"
				}
			}
			group.Buckets = append(group.Buckets, model.QuotaBucket{
				Window:            window,
				RemainingFraction: clampFraction(remaining),
				ResetTime:         isoFromAny(bucketRecord["resetTime"]),
				Description:       description,
			})
		}
		if len(group.Buckets) > 0 {
			response.Groups = append(response.Groups, group)
		}
	}
	return response
}

var groupModelsPattern = regexp.MustCompile(`(?i)^models within this group:\s*(.+)$`)

func antigravityGroupDescription(label, description string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		return ""
	}
	match := groupModelsPattern.FindStringSubmatch(description)
	if match == nil {
		return description
	}
	models := strings.Split(match[1], ",")
	cleaned := make([]string, 0, len(models))
	for _, item := range models {
		item = strings.TrimSpace(item)
		if item != "" {
			cleaned = append(cleaned, item)
		}
	}
	if strings.Contains(strings.ToLower(label), "gemini") {
		found := false
		for _, item := range cleaned {
			if strings.EqualFold(item, "Gemini Argon") {
				found = true
				break
			}
		}
		if !found {
			cleaned = append(cleaned, "Gemini Argon")
		}
	}
	return "Models within this group: " + strings.Join(cleaned, ", ")
}

func antigravityWindowKey(label, window string) string {
	text := strings.ToLower(strings.TrimSpace(window))
	if text == "" {
		text = strings.ToLower(strings.TrimSpace(label))
	}
	text = strings.TrimSuffix(text, " remaining")
	switch {
	case strings.Contains(text, "5h"), strings.Contains(text, "5 hour"), strings.Contains(text, "five hour"), strings.Contains(text, "five-hour"):
		return "five_hour"
	case strings.Contains(text, "weekly"), strings.Contains(text, "week"):
		return "weekly"
	case strings.Contains(text, "daily"), strings.Contains(text, "day"):
		return "daily"
	case strings.Contains(text, "monthly"), strings.Contains(text, "month"):
		return "monthly"
	default:
		return slugWindow(text)
	}
}

func slugWindow(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "quota"
	}
	var builder strings.Builder
	for _, char := range strings.ToLower(value) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == ' ', char == '-', char == '_':
			builder.WriteByte('_')
		}
	}
	slug := strings.Trim(builder.String(), "_")
	if slug == "" {
		return "quota"
	}
	return slug
}
