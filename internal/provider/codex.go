package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"cpa-quota/internal/model"
)

const (
	codexUsageURL        = "https://chatgpt.com/backend-api/wham/usage"
	codexResetCreditsURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits"
	codexResetConsumeURL = "https://chatgpt.com/backend-api/wham/rate-limit-reset-credits/consume"
	codexUserAgent       = "codex-tui/0.149.1 (Mac OS 26.5.2; arm64) iTerm.app/3.6.11 (codex-tui; 0.149.1)"
)

type codexProvider struct{}

func init() {
	register([]string{"codex"}, codexProvider{})
}

func (codexProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token, siwc := codexToken(ctx)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("codex credential is missing an access token")
	}
	headers := codexHeaders(ctx, token, siwc)
	resp, err := ctx.HTTP.Do(http.MethodGet, codexUsageURL, headers, nil)
	if err != nil {
		return model.QuotaFetchResponse{}, err
	}
	payload, errDecode := decodeBody(resp)
	if errDecode != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("codex usage: %w", errDecode)
	}

	group := model.QuotaGroup{DisplayName: "Codex"}
	addCodexLimit(&group, payload["rate_limit"], "", ctx)
	addCodexLimit(&group, payload["code_review_rate_limit"], "Code review", ctx)
	for _, raw := range getArray(payload, "additional_rate_limits") {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(asString(record["limit_name"]), asString(record["limitName"]), asString(record["metered_feature"]), "Additional")
		addCodexLimit(&group, record["rate_limit"], name, ctx)
	}

	response := model.QuotaFetchResponse{Groups: []model.QuotaGroup{group}}
	if plan := firstNonEmpty(asString(payload["plan_type"]), asString(payload["planType"])); plan != "" {
		response.Subscription = &model.QuotaSubscription{Plan: plan}
	}
	if credits, ok := payload["credits"].(map[string]any); ok {
		if credits["unlimited"] == true {
			response.Summary = append(response.Summary, model.QuotaMetric{Key: "credits", Label: "Credits", Value: -1, Unit: "unlimited"})
		} else if balance, okBalance := mapNumber(credits["balance"]); okBalance {
			response.Summary = append(response.Summary, model.QuotaMetric{Key: "credits", Label: "Credits", Value: balance})
		}
	}
	if count, ok := codexResetCreditCount(ctx, headers, siwc); ok {
		response.Summary = append(response.Summary, model.QuotaMetric{
			Key:   "reset_credits",
			Label: "Reset credits",
			Value: float64(count),
			Unit:  "times",
		})
	}
	return response, nil
}

func (codexProvider) Reset(ctx *Context) (model.QuotaResetResponse, error) {
	token, siwc := codexToken(ctx)
	if token == "" {
		return model.QuotaResetResponse{}, fmt.Errorf("codex credential is missing an access token")
	}
	headers := codexHeaders(ctx, token, siwc)
	count, ok := codexResetCreditCount(ctx, headers, siwc)
	if !ok || count <= 0 {
		return model.QuotaResetResponse{Success: false, Message: "no Codex reset credits available"}, nil
	}
	key := "codex|" + ctx.AuthIndex
	body, _ := json.Marshal(map[string]string{"redeem_request_id": resetRequestID(key)})
	resp, err := ctx.HTTP.Do(http.MethodPost, codexResetConsumeURL, headers, body)
	if err != nil {
		return model.QuotaResetResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			clearResetRequest(key)
		}
		return model.QuotaResetResponse{Success: false, Message: fmt.Sprintf("Codex reset returned %d", resp.StatusCode)}, nil
	}
	var payload map[string]any
	_ = json.Unmarshal(resp.Body, &payload)
	code := asString(payload["code"])
	if code != "reset" && code != "already_redeemed" {
		return model.QuotaResetResponse{Success: false, Message: "Codex reset was not confirmed"}, nil
	}
	clearResetRequest(key)
	return model.QuotaResetResponse{Success: true, Message: code}, nil
}

// codexToken returns the access token used for quota calls. With use_siwc
// enabled, CPA routes non-Codex-client traffic through the nested SIWC
// partition, so quota follows the same partition when it is available.
func codexToken(ctx *Context) (string, bool) {
	if codexUseSIWC(ctx) {
		if token := codexSIWCToken(ctx); token != "" {
			return token, true
		}
	}
	return firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "token"),
		ctx.Attributes["api_key"],
	), false
}

func codexUseSIWC(ctx *Context) bool {
	if parsed, ok := parseBoolValue(ctx.Attributes["use_siwc"]); ok {
		return parsed
	}
	for _, source := range []map[string]any{ctx.Auth, ctx.Metadata} {
		if source == nil {
			continue
		}
		if parsed, ok := parseBoolValue(source["use_siwc"]); ok {
			return parsed
		}
	}
	return false
}

func codexSIWCToken(ctx *Context) string {
	for _, source := range []map[string]any{ctx.Auth, ctx.Metadata} {
		if source == nil {
			continue
		}
		section, ok := source["siwc"].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"access_token", "access"} {
			if token := asString(section[key]); token != "" {
				return token
			}
		}
	}
	return ""
}

func parseBoolValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err != nil {
			return false, false
		}
		return parsed, true
	case float64:
		return typed != 0, true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return false, false
		}
		return parsed != 0, true
	default:
		return false, false
	}
}

func codexHeaders(ctx *Context, token string, siwc bool) map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + token,
		"Content-Type":  "application/json",
		"User-Agent":    codexUserAgent,
	}
	// SIWC requests must not carry the outer ChatGPT account header.
	if siwc {
		return headers
	}
	accountID := firstNonEmpty(
		lookupString(ctx.Metadata, "chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"),
		lookupString(ctx.Auth, "chatgpt_account_id", "chatgptAccountId", "account_id", "accountId"),
	)
	if accountID != "" {
		headers["Chatgpt-Account-Id"] = accountID
	}
	return headers
}

func addCodexLimit(group *model.QuotaGroup, raw any, prefix string, ctx *Context) {
	rate, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"primary_window", "secondary_window"} {
		window, okWindow := rate[key].(map[string]any)
		if !okWindow {
			continue
		}
		used, okUsed := mapNumber(window["used_percent"])
		if !okUsed {
			continue
		}
		seconds, _ := mapNumber(window["limit_window_seconds"])
		resetTime := isoFromAny(window["reset_at"])
		if resetTime == "" {
			if after, okAfter := mapNumber(window["reset_after_seconds"]); okAfter && after > 0 {
				resetTime = isoFromUnix(float64(ctx.Now.Unix()) + after)
			}
		}
		windowKeyValue := windowKey(seconds)
		if windowKeyValue == "" {
			windowKeyValue = key
		}
		if prefix != "" {
			windowKeyValue = strings.ReplaceAll(strings.ToLower(prefix), " ", "_") + "_" + windowKeyValue
		}
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            windowKeyValue,
			RemainingFraction: remainingFromUsedPercent(used),
			ResetTime:         resetTime,
		})
	}
}

func codexResetCreditCount(ctx *Context, headers map[string]string, siwc bool) (int, bool) {
	resetHeaders := make(map[string]string, len(headers)+3)
	for key, value := range headers {
		resetHeaders[key] = value
	}
	resetHeaders["Accept"] = "application/json"
	resetHeaders["OpenAI-Beta"] = "codex-1"
	// SIWC requests omit the Codex client originator, like the upstream executor.
	if !siwc {
		resetHeaders["Originator"] = "Codex Desktop"
	}
	resp, err := ctx.HTTP.Do(http.MethodGet, codexResetCreditsURL, resetHeaders, nil)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, false
	}
	var payload map[string]any
	if json.Unmarshal(resp.Body, &payload) != nil {
		return 0, false
	}
	if count, ok := mapNumber(payload["available_count"]); ok {
		return int(count), true
	}
	if count, ok := mapNumber(payload["availableCount"]); ok {
		return int(count), true
	}
	credits := getArray(payload, "credits")
	available := 0
	for _, raw := range credits {
		record, ok := raw.(map[string]any)
		if ok && record["status"] == "available" {
			available++
		}
	}
	return available, true
}
