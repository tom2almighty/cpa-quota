package provider

import (
	"fmt"
	"net/http"

	"cpa-quota/internal/model"
)

const metaQuotaURL = "https://api.meta.ai/muse-code/key"

type metaProvider struct{}

func init() {
	register([]string{"meta"}, metaProvider{})
}

func (metaProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "dca_token", "dcaToken"),
		lookupString(ctx.Auth, "dca_token", "dcaToken"),
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
	)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("meta credential is missing a DCA token")
	}
	resp, err := ctx.HTTP.Do(http.MethodPost, metaQuotaURL, map[string]string{
		"Accept":        "application/json",
		"Content-Type":  "application/json",
		"Authorization": "Bearer " + token,
		"x-api-version": "1.0.0",
	}, []byte("{}"))
	if err != nil {
		return model.QuotaFetchResponse{}, err
	}
	payload, errDecode := decodeBody(resp)
	if errDecode != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("meta quota: %w", errDecode)
	}

	usage := getMap(payload, "subs_usage")
	group := model.QuotaGroup{DisplayName: "Meta"}
	if usage != nil {
		addMetaBucket(&group, "session", usage["window"])
		addMetaBucket(&group, "weekly", usage["weekly"])
	}
	response := model.QuotaFetchResponse{}
	plan := firstNonEmpty(asString(payload["subs_tier_name"]), asString(getMap(usage, "tier")))
	if plan != "" {
		response.Subscription = &model.QuotaSubscription{Plan: plan}
	}
	if len(group.Buckets) > 0 {
		response.Groups = []model.QuotaGroup{group}
	}
	return response, nil
}

func (metaProvider) Reset(*Context) (model.QuotaResetResponse, error) {
	return model.QuotaResetResponse{Success: false, Message: "Meta does not support quota reset"}, nil
}

func addMetaBucket(group *model.QuotaGroup, window string, raw any) {
	record, ok := raw.(map[string]any)
	if !ok {
		return
	}
	used, okUsed := mapNumber(record["used_percent"])
	if !okUsed {
		return
	}
	bucket := model.QuotaBucket{
		Window:            window,
		RemainingFraction: remainingFromUsedPercent(used),
		ResetTime:         isoFromAny(record["resets_at"]),
	}
	if minutes, okMinutes := mapNumber(record["window_duration_mins"]); okMinutes && minutes > 0 {
		if int(minutes)%60 == 0 {
			bucket.Description = fmt.Sprintf("%d hour window", int(minutes)/60)
		} else {
			bucket.Description = fmt.Sprintf("%d min window", int(minutes))
		}
	}
	group.Buckets = append(group.Buckets, bucket)
}
