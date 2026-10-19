package provider

import (
	"encoding/json"
	"math"
	"testing"

	"cpa-quota/internal/hostapi"
	"cpa-quota/internal/model"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

type fakeResponse struct {
	status int
	body   string
}

func fakeClient(t *testing.T, routes map[string]fakeResponse) *hostapi.Client {
	t.Helper()
	call := func(method string, payload []byte) ([]byte, error) {
		if method != "host.http.do" {
			t.Fatalf("unexpected host method %s", method)
		}
		var request struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatalf("decode host request: %v", err)
		}
		response, ok := routes[request.URL]
		if !ok {
			response = fakeResponse{status: 404, body: `{"error":"not found"}`}
		}
		result, _ := json.Marshal(map[string]any{
			"StatusCode": response.status,
			"Headers":    map[string][]string{},
			"Body":       []byte(response.body),
		})
		return json.Marshal(map[string]any{
			"ok":     true,
			"result": json.RawMessage(result),
		})
	}
	return hostapi.New(call, "callback")
}

func contextFor(t *testing.T, provider, auth string, routes map[string]fakeResponse) *Context {
	t.Helper()
	return NewContext("idx", provider, []byte(auth), nil, nil, fakeClient(t, routes))
}

func findBucket(group model.QuotaGroup, window string) *model.QuotaBucket {
	for index := range group.Buckets {
		if group.Buckets[index].Window == window {
			return &group.Buckets[index]
		}
	}
	return nil
}

func TestCodexFetch(t *testing.T) {
	ctx := contextFor(t, "codex", `{"access_token":"tok","account_id":"acc"}`, map[string]fakeResponse{
		codexUsageURL: {
			status: 200,
			body: `{
				"plan_type":"plus",
				"rate_limit":{"primary_window":{"used_percent":42,"limit_window_seconds":18000,"reset_after_seconds":600},
				              "secondary_window":{"used_percent":7,"limit_window_seconds":604800,"reset_at":1790500000}},
				"credits":{"balance":"12.5"}
			}`,
		},
		codexResetCreditsURL: {
			status: 200,
			body:   `{"available_count":2}`,
		},
	})
	response, err := codexProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Plan != "plus" {
		t.Fatalf("plan = %#v", response.Subscription)
	}
	if len(response.Groups) != 1 || len(response.Groups[0].Buckets) != 2 {
		t.Fatalf("groups = %#v", response.Groups)
	}
	if bucket := findBucket(response.Groups[0], "five_hour"); bucket == nil || !near(bucket.RemainingFraction, 0.58) {
		t.Fatalf("five hour bucket = %#v", bucket)
	}
	if bucket := findBucket(response.Groups[0], "weekly"); bucket == nil || !near(bucket.RemainingFraction, 0.93) {
		t.Fatalf("weekly bucket = %#v", bucket)
	}
	if len(response.Summary) != 2 {
		t.Fatalf("summary = %#v", response.Summary)
	}
}

func TestClaudeFetch(t *testing.T) {
	ctx := contextFor(t, "claude", `{"access_token":"tok"}`, map[string]fakeResponse{
		claudeUsageURL: {
			status: 200,
			body: `{
				"five_hour":{"utilization":80,"resets_at":"2026-09-24T05:00:00Z"},
				"seven_day":{"utilization":20,"resets_at":"2026-09-30T00:00:00Z"},
				"seven_day_opus":null,
				"extra_usage":{"is_enabled":true,"monthly_limit":20000,"used_credits":4200,"utilization":21}
			}`,
		},
		claudeProfileURL: {
			status: 200,
			body:   `{"account":{"has_claude_max":true},"organization":{"uuid":"00000000-0000-0000-0000-000000000001"}}`,
		},
		claudeGrantURL: {
			status: 200,
			body:   `{"cedar_ember":{"eligible":true,"at_limit":true,"grants":[{"id":"g1","resets_left":2}]}}`,
		},
	})
	response, err := claudeProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Plan != "Max" {
		t.Fatalf("plan = %#v", response.Subscription)
	}
	if bucket := findBucket(response.Groups[0], "five_hour"); bucket == nil || !near(bucket.RemainingFraction, 0.2) {
		t.Fatalf("five hour bucket = %#v", bucket)
	}
	if bucket := findBucket(response.Groups[0], "extra_usage"); bucket == nil || bucket.Description != "$42.00 / $200.00" {
		t.Fatalf("extra usage bucket = %#v", bucket)
	}
	if len(response.Summary) != 1 || response.Summary[0].Value != 2 {
		t.Fatalf("summary = %#v", response.Summary)
	}
}

func TestAntigravityFetch(t *testing.T) {
	ctx := contextFor(t, "antigravity", `{"access_token":"tok","installed":{"project_id":"proj"}}`, map[string]fakeResponse{
		antigravityQuotaURLs[0]: {
			status: 200,
			body: `{"groups":[{"displayName":"Gemini Models","description":"Models within this group: Gemini Flash",
				"buckets":[{"displayName":"Weekly Limit","window":"weekly","remainingFraction":0},
				           {"displayName":"Five Hour Limit","window":"5h","remainingFraction":1}]}]}`,
		},
	})
	response, err := antigravityProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(response.Groups) != 1 || len(response.Groups[0].Buckets) != 2 {
		t.Fatalf("groups = %#v", response.Groups)
	}
	if bucket := findBucket(response.Groups[0], "five_hour"); bucket == nil || bucket.RemainingFraction != 1 {
		t.Fatalf("five hour bucket = %#v", bucket)
	}
	if response.Groups[0].Buckets[0].Description == "" {
		t.Fatalf("description is empty")
	}
}

func TestKimiFetch(t *testing.T) {
	ctx := contextFor(t, "kimi", `{"access_token":"tok"}`, map[string]fakeResponse{
		kimiUsageURL: {
			status: 200,
			body: `{"usage":{"used":30,"limit":100,"resetAt":"2026-09-30T00:00:00Z"},
				"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"remaining":15,"limit":20}}]}`,
		},
	})
	response, err := kimiProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(response.Groups) != 1 || len(response.Groups[0].Buckets) != 2 {
		t.Fatalf("groups = %#v", response.Groups)
	}
	if bucket := findBucket(response.Groups[0], "weekly"); bucket == nil || !near(bucket.RemainingFraction, 0.7) {
		t.Fatalf("weekly bucket = %#v", bucket)
	}
}

func TestXaiFetch(t *testing.T) {
	ctx := contextFor(t, "xai", `{"access_token":"tok","sub":"user"}`, map[string]fakeResponse{
		xaiWeeklyURL: {
			status: 200,
			body:   `{"config":{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","end":"2026-09-30T00:00:00Z"},"creditUsagePercent":60}}`,
		},
		xaiMonthlyURL: {
			status: 200,
			body:   `{"config":{"monthlyLimit":15000,"used":20000,"onDemandCap":10000,"billingPeriodEnd":"2026-10-01T00:00:00Z"}}`,
		},
	})
	response, err := xaiProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Plan != "SuperGrok" {
		t.Fatalf("plan = %#v", response.Subscription)
	}
	if bucket := findBucket(response.Groups[0], "weekly"); bucket == nil || !near(bucket.RemainingFraction, 0.4) {
		t.Fatalf("weekly bucket = %#v", bucket)
	}
	if bucket := findBucket(response.Groups[0], "on_demand"); bucket == nil || !near(bucket.RemainingFraction, 0.5) {
		t.Fatalf("on demand bucket = %#v", bucket)
	}
}

func TestDevinFetch(t *testing.T) {
	ctx := contextFor(t, "devin", `{"access_token":"tok"}`, map[string]fakeResponse{
		devinStatusURL: {
			status: 200,
			body: `{"userStatus":{"planStatus":{"planInfo":{"planName":"Pro"},
				"dailyQuotaRemainingPercent":85,"dailyQuotaResetAtUnix":1790500000,
				"weeklyQuotaRemainingPercent":40,"weeklyQuotaResetAtUnix":1791000000}}}`,
		},
	})
	response, err := devinProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Plan != "Pro" {
		t.Fatalf("plan = %#v", response.Subscription)
	}
	if bucket := findBucket(response.Groups[0], "daily"); bucket == nil || !near(bucket.RemainingFraction, 0.85) {
		t.Fatalf("daily bucket = %#v", bucket)
	}
}

func TestMetaFetch(t *testing.T) {
	ctx := contextFor(t, "meta", `{"dca_token":"tok"}`, map[string]fakeResponse{
		metaQuotaURL: {
			status: 200,
			body: `{"subs_tier_name":"Pro","subs_usage":{"window":{"used_percent":35,"resets_at":1790500000,"window_duration_mins":180},
				"weekly":{"used_percent":70,"resets_at":1791000000}}}`,
		},
	})
	response, err := metaProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if response.Subscription == nil || response.Subscription.Plan != "Pro" {
		t.Fatalf("plan = %#v", response.Subscription)
	}
	if bucket := findBucket(response.Groups[0], "session"); bucket == nil || !near(bucket.RemainingFraction, 0.65) {
		t.Fatalf("session bucket = %#v", bucket)
	}
	if bucket := findBucket(response.Groups[0], "session"); bucket.Description != "3 hour window" {
		t.Fatalf("session description = %q", bucket.Description)
	}
}
