package provider

import (
	"fmt"
	"net/http"
	"strings"

	"cpa-quota/internal/model"
)

const (
	kimiUsageURL   = "https://api.kimi.com/coding/v1/usages"
	kimiAIUsageURL = "https://api.kimi.ai/coding/v1/usages"
)

type kimiProvider struct{}

func init() {
	register([]string{"kimi", "kimi-ai"}, kimiProvider{})
}

func (kimiProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "token"),
		ctx.Attributes["api_key"],
	)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("kimi credential is missing an access token")
	}
	url := kimiUsageURL
	if ctx.Provider == "kimi-ai" {
		url = kimiAIUsageURL
	}
	resp, err := ctx.HTTP.Do(http.MethodGet, url, map[string]string{
		"Authorization": "Bearer " + token,
	}, nil)
	if err != nil {
		return model.QuotaFetchResponse{}, err
	}
	payload, errDecode := decodeBody(resp)
	if errDecode != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("kimi usage: %w", errDecode)
	}

	group := model.QuotaGroup{DisplayName: "Kimi"}
	if usage, ok := payload["usage"].(map[string]any); ok {
		addKimiBucket(&group, "weekly", "", usage, usage, ctx)
	}
	for index, raw := range getArray(payload, "limits") {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		window := record
		if nested, okNested := record["window"].(map[string]any); okNested {
			window = nested
		}
		duration, _ := mapNumber(window["duration"])
		unit := asString(window["timeUnit"])
		windowKeyValue := windowKey(kimiDurationSeconds(duration, unit))
		if windowKeyValue == "" {
			windowKeyValue = fmt.Sprintf("limit_%d", index)
		}
		detail, okDetail := record["detail"].(map[string]any)
		if !okDetail {
			detail = record
		}
		addKimiBucket(&group, windowKeyValue, firstNonEmpty(asString(record["name"]), asString(record["title"]), ""), detail, record, ctx)
	}

	response := model.QuotaFetchResponse{}
	if len(group.Buckets) > 0 {
		response.Groups = []model.QuotaGroup{group}
	}
	return response, nil
}

func (kimiProvider) Reset(*Context) (model.QuotaResetResponse, error) {
	return model.QuotaResetResponse{Success: false, Message: "Kimi does not support quota reset"}, nil
}

func addKimiBucket(group *model.QuotaGroup, windowKeyValue, label string, detail, resetSource map[string]any, ctx *Context) {
	limit, okLimit := mapNumber(detail["limit"])
	remaining, okRemaining := mapNumber(detail["remaining"])
	used, okUsed := mapNumber(detail["used"])
	if !okUsed && okLimit && okRemaining {
		used = limit - remaining
		okUsed = true
	}
	if !okLimit || !okUsed {
		return
	}
	resetTime := kimiResetTime(detail, ctx)
	if resetTime == "" {
		resetTime = kimiResetTime(resetSource, ctx)
	}
	fraction := 1.0
	if limit > 0 {
		fraction = clampFraction(1 - used/limit)
	}
	group.Buckets = append(group.Buckets, model.QuotaBucket{
		Window:            windowKeyValue,
		RemainingFraction: fraction,
		ResetTime:         resetTime,
		Description:       label,
	})
}

func kimiDurationSeconds(duration float64, unit string) float64 {
	if duration <= 0 {
		return 0
	}
	unit = strings.ToUpper(strings.TrimSpace(unit))
	unit = strings.TrimPrefix(unit, "TIME_UNIT_")
	unit = strings.TrimSuffix(unit, "S")
	switch unit {
	case "MINUTE":
		return duration * 60
	case "HOUR":
		return duration * 3600
	case "DAY":
		return duration * 86400
	default:
		return duration
	}
}

func kimiResetTime(record map[string]any, ctx *Context) string {
	for _, key := range []string{"resetAt", "reset_at", "resetTime", "reset_time"} {
		if value := isoFromAny(record[key]); value != "" {
			return value
		}
	}
	for _, key := range []string{"resetIn", "reset_in", "ttl"} {
		if seconds, ok := mapNumber(record[key]); ok && seconds > 0 {
			return isoFromUnix(float64(ctx.Now.Unix()) + seconds)
		}
	}
	return ""
}
