package provider

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cpa-quota/internal/model"
)

const (
	copilotUserURL = "https://api.github.com/copilot_internal/user"
	// copilotUserAgent mirrors CLIProxyAPI's Copilot CLI baseline (internal/auth/copilot/client.go).
	copilotUserAgent = "copilot/1.0.114"
)

var copilotTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

type copilotProvider struct{}

func init() {
	register([]string{"copilot", "github-copilot"}, copilotProvider{})
}

func (copilotProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	// Copilot's access_token is short-lived and scoped to inference; only
	// refresh_token holds the GitHub OAuth token accepted by /copilot_internal/user.
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "refresh_token", "refreshToken"),
		lookupString(ctx.Auth, "refresh_token", "refreshToken"),
	)
	if token == "" || !copilotTokenPattern.MatchString(token) {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot credential is missing a valid GitHub OAuth token")
	}

	resp, err := ctx.HTTP.Do(http.MethodGet, copilotUserURL, map[string]string{
		"Authorization": "Bearer " + token,
		"User-Agent":    copilotUserAgent,
		"Accept":        "application/json",
	}, nil)
	if err != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot quota request failed")
	}
	if resp == nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot quota returned an empty response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot quota returned %d", resp.StatusCode)
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body, &payload); errDecode != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot quota returned invalid JSON")
	}
	return parseCopilotQuota(payload)
}

func (copilotProvider) Reset(*Context) (model.QuotaResetResponse, error) {
	return model.QuotaResetResponse{Success: false, Message: "Copilot does not support quota reset"}, nil
}

func parseCopilotQuota(payload map[string]any) (model.QuotaFetchResponse, error) {
	snapshots := getMap(payload, "quota_snapshots")
	premium := getMap(snapshots, "premium_interactions")
	if premium == nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot quota is unavailable")
	}

	response := model.QuotaFetchResponse{}
	if plan := asString(payload["copilot_plan"]); plan != "" {
		response.Subscription = &model.QuotaSubscription{Plan: plan}
	}

	if unlimited, ok := premium["unlimited"].(bool); ok && unlimited {
		response.Summary = append(response.Summary, model.QuotaMetric{
			Key:   "premium_interactions",
			Label: "Premium interactions",
			Value: -1,
			Unit:  "unlimited",
		})
		return response, nil
	}

	percentRemaining, okPercent := copilotNumber(premium["percent_remaining"])
	if !okPercent || percentRemaining > 100 {
		return model.QuotaFetchResponse{}, fmt.Errorf("copilot quota is unavailable")
	}

	description := ""
	remaining, okRemaining := copilotNumber(premium["quota_remaining"])
	entitlement, okEntitlement := copilotNumber(premium["entitlement"])
	if okRemaining && okEntitlement && entitlement > 0 {
		description = fmt.Sprintf("%s / %s", formatCopilotNumber(remaining), formatCopilotNumber(entitlement))
	}

	response.Groups = []model.QuotaGroup{{
		DisplayName: "Copilot",
		Buckets: []model.QuotaBucket{{
			Window:            "premium_interactions",
			RemainingFraction: clampFraction(percentRemaining / 100),
			ResetTime:         copilotResetTime(payload),
			Description:       description,
		}},
	}}
	return response, nil
}

func copilotNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed < 0 {
			return 0, false
		}
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return 0, false
		}
		return number, true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		number, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 {
			return 0, false
		}
		return number, true
	default:
		return 0, false
	}
}

func formatCopilotNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func copilotResetTime(payload map[string]any) string {
	for _, key := range []string{"quota_reset_date_utc", "quota_reset_date"} {
		if ts := isoFromAny(payload[key]); ts != "" {
			return ts
		}
		raw := asString(payload[key])
		if raw == "" {
			continue
		}
		if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
	}
	return ""
}
