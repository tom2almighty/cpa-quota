package provider

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"cpa-quota/internal/model"
)

const (
	xaiWeeklyURL     = "https://cli-chat-proxy.grok.com/v1/billing?format=credits"
	xaiMonthlyURL    = "https://cli-chat-proxy.grok.com/v1/billing"
	xaiClientVersion = "1.0.46"
)

type xaiProvider struct{}

func init() {
	register([]string{"xai", "grok"}, xaiProvider{})
}

func (xaiProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "token"),
		ctx.Attributes["api_key"],
	)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("xai credential is missing an access token")
	}
	headers := map[string]string{
		"Authorization":         "Bearer " + token,
		"x-xai-token-auth":      "xai-grok-cli",
		"x-grok-client-version": xaiClientVersion,
		"accept":                "*/*",
		"user-agent":            fmt.Sprintf("grok-pager/%s grok-shell/%s (macos; aarch64)", xaiClientVersion, xaiClientVersion),
	}
	userID := firstNonEmpty(
		lookupString(ctx.Auth, "sub", "user_id", "userId"),
		lookupString(ctx.Metadata, "sub", "user_id", "userId"),
	)
	if userID != "" {
		headers["x-userid"] = userID
	}

	weekly, errWeekly := xaiPayload(ctx, xaiWeeklyURL, headers)
	if errWeekly != nil {
		return model.QuotaFetchResponse{}, errWeekly
	}
	monthly, errMonthly := xaiPayload(ctx, xaiMonthlyURL, headers)
	if errMonthly != nil {
		monthly = nil
	}

	group := model.QuotaGroup{DisplayName: "xAI"}
	response := model.QuotaFetchResponse{}

	if config, ok := weekly["config"].(map[string]any); ok {
		weekly = config
	}
	period, _ := weekly["currentPeriod"].(map[string]any)
	if period == nil {
		period, _ = weekly["current_period"].(map[string]any)
	}
	periodType := strings.ToLower(asString(period["type"]))
	periodEnd := isoFromAny(period["end"])
	percent, okPercent := mapNumber(weekly["creditUsagePercent"])
	if !okPercent {
		percent, okPercent = mapNumber(weekly["credit_usage_percent"])
	}
	if okPercent {
		window := "current_period"
		if strings.Contains(periodType, "week") {
			window = "weekly"
		} else if strings.Contains(periodType, "month") {
			window = "monthly"
		}
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            window,
			RemainingFraction: remainingFromUsedPercent(percent),
			ResetTime:         periodEnd,
		})
	}
	products := getArray(weekly, "productUsage")
	if products == nil {
		products = getArray(weekly, "product_usage")
	}
	for _, raw := range products {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := asString(record["product"])
		percent, okPercent := mapNumber(record["usagePercent"])
		if !okPercent {
			percent, okPercent = mapNumber(record["usage_percent"])
		}
		if name == "" || !okPercent {
			continue
		}
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            slugWindow(name),
			RemainingFraction: remainingFromUsedPercent(percent),
			ResetTime:         periodEnd,
		})
	}

	if config, ok := monthly["config"].(map[string]any); ok {
		monthly = config
	}
	limit, okLimit := mapNumber(monthly["monthlyLimit"])
	if !okLimit {
		limit, okLimit = mapNumber(monthly["monthly_limit"])
	}
	used, okUsed := mapNumber(monthly["used"])
	if okLimit && limit > 0 && okUsed {
		included := math.Min(used, limit)
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            "monthly",
			RemainingFraction: clampFraction(1 - included/limit),
			ResetTime:         isoFromAny(firstNonNil(monthly["billingPeriodEnd"], monthly["billing_period_end"])),
			Description:       fmt.Sprintf("$%.2f / $%.2f", included/100, limit/100),
		})
	}
	onDemandCap, okCap := mapNumber(monthly["onDemandCap"])
	if !okCap {
		onDemandCap, okCap = mapNumber(monthly["on_demand_cap"])
	}
	onDemandUsed, okOnDemand := mapNumber(monthly["onDemandUsed"])
	if !okOnDemand {
		onDemandUsed, okOnDemand = mapNumber(monthly["on_demand_used"])
	}
	if okLimit && okUsed && !okOnDemand {
		onDemandUsed, okOnDemand = math.Max(0, used-limit), true
	}
	if okCap && onDemandCap > 0 && okOnDemand {
		group.Buckets = append(group.Buckets, model.QuotaBucket{
			Window:            "on_demand",
			RemainingFraction: clampFraction(1 - onDemandUsed/onDemandCap),
			ResetTime:         isoFromAny(firstNonNil(monthly["billingPeriodEnd"], monthly["billing_period_end"])),
			Description:       fmt.Sprintf("$%.2f / $%.2f", onDemandUsed/100, onDemandCap/100),
		})
	} else if okCap && onDemandCap == 0 {
		response.Summary = append(response.Summary, model.QuotaMetric{
			Key:   "on_demand",
			Label: "Pay as you go",
			Value: 0,
			Unit:  "disabled",
		})
	}

	if okLimit {
		switch limit {
		case 15000:
			response.Subscription = &model.QuotaSubscription{Plan: "SuperGrok"}
		case 150000:
			response.Subscription = &model.QuotaSubscription{Plan: "SuperGrok Heavy"}
		}
	}
	if len(group.Buckets) > 0 {
		response.Groups = []model.QuotaGroup{group}
	}
	return response, nil
}

func (xaiProvider) Reset(*Context) (model.QuotaResetResponse, error) {
	return model.QuotaResetResponse{Success: false, Message: "xAI does not support quota reset"}, nil
}

func xaiPayload(ctx *Context, url string, headers map[string]string) (map[string]any, error) {
	resp, err := ctx.HTTP.Do(http.MethodGet, url, headers, nil)
	if err != nil {
		return nil, err
	}
	return decodeBody(resp)
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
