package provider

import (
	"encoding/json"
	"fmt"
	"net/http"

	"cpa-quota/internal/model"
)

const devinStatusURL = "https://server.codeium.com/exa.seat_management_pb.SeatManagementService/GetUserStatus"

type devinProvider struct{}

func init() {
	register([]string{"devin"}, devinProvider{})
}

func (devinProvider) Fetch(ctx *Context) (model.QuotaFetchResponse, error) {
	token := firstNonEmpty(
		lookupString(ctx.Metadata, "access_token", "accessToken"),
		lookupString(ctx.Auth, "access_token", "accessToken"),
		lookupString(ctx.Metadata, "api_key", "apiKey"),
		lookupString(ctx.Auth, "api_key", "apiKey"),
		ctx.Attributes["api_key"],
	)
	if token == "" {
		return model.QuotaFetchResponse{}, fmt.Errorf("devin credential is missing an access token")
	}
	body, _ := json.Marshal(map[string]any{
		"metadata": map[string]string{
			"ideName":          "chisel",
			"ideVersion":       "3000.10.21",
			"apiKey":           token,
			"locale":           "en",
			"os":               "darwin",
			"extensionVersion": "3000.10.21",
			"clientName":       "chisel",
		},
	})
	resp, err := ctx.HTTP.Do(http.MethodPost, devinStatusURL, map[string]string{
		"Content-Type":             "application/json",
		"Connect-Protocol-Version": "1",
	}, body)
	if err != nil {
		return model.QuotaFetchResponse{}, err
	}
	payload, errDecode := decodeBody(resp)
	if errDecode != nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("devin status: %w", errDecode)
	}

	status := getMap(getMap(payload, "userStatus"), "planStatus")
	if status == nil {
		return model.QuotaFetchResponse{}, fmt.Errorf("devin returned no plan status")
	}
	group := model.QuotaGroup{DisplayName: "Devin"}
	addDevinBucket(&group, "daily", status["dailyQuotaRemainingPercent"], status["dailyQuotaResetAtUnix"])
	addDevinBucket(&group, "weekly", status["weeklyQuotaRemainingPercent"], status["weeklyQuotaResetAtUnix"])

	response := model.QuotaFetchResponse{}
	if plan := asString(getMap(status, "planInfo")["planName"]); plan != "" {
		response.Subscription = &model.QuotaSubscription{Plan: plan}
	}
	if len(group.Buckets) > 0 {
		response.Groups = []model.QuotaGroup{group}
	}
	return response, nil
}

func (devinProvider) Reset(*Context) (model.QuotaResetResponse, error) {
	return model.QuotaResetResponse{Success: false, Message: "Devin does not support quota reset"}, nil
}

func addDevinBucket(group *model.QuotaGroup, window string, remainingRaw, resetRaw any) {
	remaining, okRemaining := mapNumber(remainingRaw)
	resetSeconds, okReset := mapNumber(resetRaw)
	if !okRemaining && !okReset {
		return
	}
	bucket := model.QuotaBucket{Window: window}
	if okRemaining {
		bucket.RemainingFraction = clampFraction(remaining / 100)
	}
	if okReset && resetSeconds > 0 {
		bucket.ResetTime = isoFromUnix(resetSeconds)
	}
	group.Buckets = append(group.Buckets, bucket)
}
