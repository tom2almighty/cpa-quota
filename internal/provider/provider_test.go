package provider

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"

	"cpa-quota/internal/hostapi"
	"cpa-quota/internal/model"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

type fakeResponse struct {
	status int
	body   string
}

type capturedCall struct {
	method string
	header http.Header
}

func fakeClient(t *testing.T, routes map[string]fakeResponse) *hostapi.Client {
	t.Helper()
	return fakeClientCapture(t, routes, nil)
}

func fakeClientCapture(t *testing.T, routes map[string]fakeResponse, captured map[string]capturedCall) *hostapi.Client {
	t.Helper()
	call := func(method string, payload []byte) ([]byte, error) {
		if method != "host.http.do" {
			t.Fatalf("unexpected host method %s", method)
		}
		var request struct {
			Method  string              `json:"method"`
			URL     string              `json:"url"`
			Headers map[string][]string `json:"headers"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			t.Fatalf("decode host request: %v", err)
		}
		if captured != nil {
			captured[request.URL] = capturedCall{method: request.Method, header: http.Header(request.Headers)}
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

func contextForCapture(t *testing.T, provider, auth string, attributes map[string]string, routes map[string]fakeResponse, captured map[string]capturedCall) *Context {
	t.Helper()
	return NewContext("idx", provider, []byte(auth), nil, attributes, fakeClientCapture(t, routes, captured))
}

func codexRoutes() map[string]fakeResponse {
	return map[string]fakeResponse{
		codexUsageURL:        {status: 200, body: `{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":18000,"reset_after_seconds":600}}}`},
		codexResetCreditsURL: {status: 200, body: `{"available_count":1}`},
		codexResetConsumeURL: {status: 200, body: `{"code":"reset"}`},
	}
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

func TestCodexFetchUsesSIWC(t *testing.T) {
	captured := map[string]capturedCall{}
	ctx := contextForCapture(t, "codex",
		`{"access_token":"outer","account_id":"outer-acc","use_siwc":true,"siwc":{"access_token":"siwc-token"}}`,
		nil, codexRoutes(), captured)
	if _, err := (codexProvider{}).Fetch(ctx); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	usage := captured[codexUsageURL]
	if got := usage.header.Get("Authorization"); got != "Bearer siwc-token" {
		t.Fatalf("usage authorization = %q", got)
	}
	if got := usage.header.Get("Chatgpt-Account-Id"); got != "" {
		t.Fatalf("usage account header = %q", got)
	}
	resetCredits := captured[codexResetCreditsURL]
	if got := resetCredits.header.Get("Authorization"); got != "Bearer siwc-token" {
		t.Fatalf("reset credits authorization = %q", got)
	}
	if got := resetCredits.header.Get("Originator"); got != "" {
		t.Fatalf("reset credits originator = %q", got)
	}

	if _, err := (codexProvider{}).Reset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	consume := captured[codexResetConsumeURL]
	if consume.method != http.MethodPost || consume.header.Get("Authorization") != "Bearer siwc-token" {
		t.Fatalf("consume call = %#v", consume)
	}
}

func TestCodexFetchSIWCFallsBackToOuterToken(t *testing.T) {
	captured := map[string]capturedCall{}
	ctx := contextForCapture(t, "codex",
		`{"access_token":"outer","account_id":"outer-acc","use_siwc":true}`,
		nil, codexRoutes(), captured)
	if _, err := (codexProvider{}).Fetch(ctx); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	usage := captured[codexUsageURL]
	if got := usage.header.Get("Authorization"); got != "Bearer outer" {
		t.Fatalf("usage authorization = %q", got)
	}
	if got := usage.header.Get("Chatgpt-Account-Id"); got != "outer-acc" {
		t.Fatalf("usage account header = %q", got)
	}
	if got := captured[codexResetCreditsURL].header.Get("Originator"); got != "Codex Desktop" {
		t.Fatalf("reset credits originator = %q", got)
	}
}

func TestCodexFetchSIWCFromAttributes(t *testing.T) {
	captured := map[string]capturedCall{}
	ctx := contextForCapture(t, "codex",
		`{"access_token":"outer","account_id":"outer-acc","siwc":{"access_token":"siwc-token"}}`,
		map[string]string{"use_siwc": "true"}, codexRoutes(), captured)
	if _, err := (codexProvider{}).Fetch(ctx); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := captured[codexUsageURL].header.Get("Authorization"); got != "Bearer siwc-token" {
		t.Fatalf("usage authorization = %q", got)
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

func TestClaudeFetchPrefersFableLimit(t *testing.T) {
	ctx := contextFor(t, "claude", `{"access_token":"tok"}`, map[string]fakeResponse{
		claudeUsageURL: {
			status: 200,
			body: `{
				"five_hour":{"utilization":10,"resets_at":"2026-09-24T05:00:00Z"},
				"limits":[{"kind":"weekly_scoped","is_active":true,"percent":30,"resets_at":"2026-09-30T00:00:00Z",
					"scope":{"model":{"display_name":"Fable"}}}],
				"iguana_necktie":{"utilization":99,"resets_at":"2026-10-01T00:00:00Z"}
			}`,
		},
	})
	response, err := claudeProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if bucket := findBucket(response.Groups[0], "seven_day_fable"); bucket == nil || !near(bucket.RemainingFraction, 0.7) {
		t.Fatalf("fable bucket = %#v", bucket)
	}
}

func TestClaudeFetchCloudSessionCredits(t *testing.T) {
	ctx := contextFor(t, "claude", `{"access_token":"tok"}`, map[string]fakeResponse{
		claudeUsageURL: {
			status: 200,
			body: `{
				"five_hour":{"utilization":10,"resets_at":"2026-09-24T05:00:00Z"},
				"iguana_necktie":{"limit_dollars":100,"used_dollars":5.043595,"remaining_dollars":94.956405,
					"utilization":5.043595,"resets_at":"2026-11-01T00:00:00Z"},
				"additional_credit_balance":{"limit_dollars":20,"used_dollars":0,"remaining_dollars":20,
					"utilization":0,"resets_at":null}
			}`,
		},
	})
	response, err := claudeProvider{}.Fetch(ctx)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	bucket := findBucket(response.Groups[0], "cloud_session_credits")
	if bucket == nil || !near(bucket.RemainingFraction, 0.94956405) || bucket.Description != "$5.04 / $100.00" {
		t.Fatalf("cloud session credits bucket = %#v", bucket)
	}
	if findBucket(response.Groups[0], "seven_day_fable") != nil {
		t.Fatalf("dollar-denominated iguana_necktie must not become a Fable window")
	}
	if balance := findBucket(response.Groups[0], "additional_credit_balance"); balance == nil || !near(balance.RemainingFraction, 1) {
		t.Fatalf("additional credit balance = %#v", balance)
	}
}

func TestClaudeReset(t *testing.T) {
	const organization = "00000000-0000-0000-0000-000000000001"
	claimURL := "https://api.anthropic.com/api/organizations/" + organization + "/reset_rate_limits"
	captured := map[string]capturedCall{}
	ctx := contextForCapture(t, "claude", `{"access_token":"tok"}`, nil, map[string]fakeResponse{
		claudeGrantURL: {
			status: 200,
			body:   `{"cedar_ember":{"eligible":true,"at_limit":true,"grants":[{"id":"grant-1","resets_left":1,"usable_now":true}]}}`,
		},
		claudeProfileURL: {
			status: 200,
			body:   `{"organization":{"uuid":"00000000-0000-0000-0000-000000000001"}}`,
		},
		claimURL: {
			status: 200,
			body:   `{"result":"reset"}`,
		},
	}, captured)

	response, err := (claudeProvider{}).Reset(ctx)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if !response.Success || response.Message != "reset" {
		t.Fatalf("reset response = %#v", response)
	}
	claim := captured[claimURL]
	if claim.method != http.MethodPost || claim.header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("claim call = %#v", claim)
	}
}

func TestClaudeResetStatusFailures(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		message string
	}{
		{name: "rate limited", status: http.StatusTooManyRequests, message: "rate limited by Anthropic"},
		{name: "unauthorized", status: http.StatusUnauthorized, message: "Claude authentication failed"},
		{name: "forbidden", status: http.StatusForbidden, message: "Claude authentication failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := contextFor(t, "claude", `{"access_token":"tok"}`, map[string]fakeResponse{
				claudeGrantURL: {status: test.status, body: `{}`},
			})
			response, err := (claudeProvider{}).Reset(ctx)
			if err != nil {
				t.Fatalf("reset: %v", err)
			}
			if response.Success || response.Message != test.message {
				t.Fatalf("reset response = %#v", response)
			}
		})
	}
}

func TestClaudeResetProfileAuthFailure(t *testing.T) {
	ctx := contextFor(t, "claude", `{"access_token":"tok"}`, map[string]fakeResponse{
		claudeGrantURL: {
			status: 200,
			body:   `{"cedar_ember":{"eligible":true,"at_limit":true,"grants":[{"id":"grant-1","resets_left":1,"usable_now":true}]}}`,
		},
		claudeProfileURL: {status: http.StatusUnauthorized, body: `{}`},
	})
	response, err := (claudeProvider{}).Reset(ctx)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}
	if response.Success || response.Message != "Claude authentication failed" {
		t.Fatalf("reset response = %#v", response)
	}
}

func TestClaudeResetRejectsMalformedIdentity(t *testing.T) {
	tests := []struct {
		name         string
		grantID      string
		organization string
		message      string
		calls        int
	}{
		{
			name:         "grant id",
			grantID:      "../grant",
			organization: "00000000-0000-0000-0000-000000000001",
			message:      "Claude reset grant id is invalid",
			calls:        1,
		},
		{
			name:         "organization id",
			grantID:      "grant-1",
			organization: "../organization",
			message:      "Claude organization id is unavailable",
			calls:        2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			captured := map[string]capturedCall{}
			ctx := contextForCapture(t, "claude", `{"access_token":"tok"}`, nil, map[string]fakeResponse{
				claudeGrantURL: {
					status: 200,
					body:   fmt.Sprintf(`{"cedar_ember":{"eligible":true,"at_limit":true,"grants":[{"id":%q,"resets_left":1,"usable_now":true}]}}`, test.grantID),
				},
				claudeProfileURL: {
					status: 200,
					body:   fmt.Sprintf(`{"organization":{"uuid":%q}}`, test.organization),
				},
			}, captured)
			response, err := (claudeProvider{}).Reset(ctx)
			if err != nil {
				t.Fatalf("reset: %v", err)
			}
			if response.Success || response.Message != test.message {
				t.Fatalf("reset response = %#v", response)
			}
			if len(captured) != test.calls {
				t.Fatalf("claim should not be sent, captured calls = %#v", captured)
			}
		})
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

func TestKimiPlan(t *testing.T) {
	for _, provider := range []string{"kimi", "kimi-ai"} {
		for _, tc := range []struct {
			name   string
			status int
			body   string
			plan   string
		}{
			{"valid", 200, `{"user_id":"user","user_level_name":"  Allegretto  "}`, "Allegretto"},
			{"missing user", 200, `{"user_level_name":"Allegretto"}`, ""},
			{"empty user", 200, `{"user_id":" ","user_level_name":"Allegretto"}`, ""},
			{"invalid user", 200, `{"user_id":123,"user_level_name":"Allegretto"}`, ""},
			{"invalid plan", 200, `{"user_id":"user","user_level_name":123}`, ""},
			{"empty plan", 200, `{"user_id":"user","user_level_name":" "}`, ""},
			{"unavailable", 503, `{"error":"unavailable"}`, ""},
			{"invalid JSON", 200, `invalid`, ""},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				usageURL := kimiUsageURL
				profileURL := "https://api.kimi.com/coding/v1/me"
				if provider == "kimi-ai" {
					usageURL = kimiAIUsageURL
					profileURL = "https://api.kimi.ai/coding/v1/me"
				}
				captured := map[string]capturedCall{}
				ctx := contextForCapture(t, provider, `{"access_token":"tok"}`, nil, map[string]fakeResponse{
					usageURL:   {status: 200, body: `{"usage":{"used":30,"limit":100}}`},
					profileURL: {status: tc.status, body: tc.body},
				}, captured)
				response, err := kimiProvider{}.Fetch(ctx)
				if err != nil {
					t.Fatalf("fetch: %v", err)
				}
				if len(response.Groups) != 1 || !near(response.Groups[0].Buckets[0].RemainingFraction, 0.7) {
					t.Fatalf("quota lost: %#v", response.Groups)
				}
				if tc.plan == "" {
					if response.Subscription != nil {
						t.Fatalf("unexpected subscription: %#v", response.Subscription)
					}
				} else if response.Subscription == nil || response.Subscription.Plan != tc.plan {
					t.Fatalf("subscription = %#v, want %s", response.Subscription, tc.plan)
				}
				if len(captured) != 2 || captured[profileURL].method != http.MethodGet ||
					captured[profileURL].header.Get("Authorization") != "Bearer tok" ||
					captured[profileURL].header.Get("Accept") != "application/json" {
					t.Fatalf("unexpected profile request: %#v", captured)
				}
			})
		}
	}
}

func TestClaudeSubscription(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		plan    string
	}{
		{"max 5x", `{"account":{"has_claude_max":true},"organization":{"rate_limit_tier":"default_claude_max_5x"}}`, "Max 5x"},
		{"max 20x", `{"account":{"has_claude_max":true},"organization":{"rate_limit_tier":"  DEFAULT_CLAUDE_MAX_20X  "}}`, "Max 20x"},
		{"active team", `{"account":{"has_claude_max":true},"organization":{"organization_type":"claude_team","subscription_status":"active","rate_limit_tier":"default_claude_max_20x"}}`, "Team"},
		{"personal max", `{"account":{"has_claude_max":true},"organization":{"organization_type":"claude_team","subscription_status":"inactive","rate_limit_tier":"default_claude_ai"}}`, "Max"},
		{"pro", `{"account":{"has_claude_pro":true},"organization":{"rate_limit_tier":"default_claude_ai"}}`, "Pro"},
		{"free", `{"account":{"has_claude_max":false,"has_claude_pro":false},"organization":{"rate_limit_tier":"default_claude_ai"}}`, "Free"},
		{"unknown", `{"organization":{"rate_limit_tier":"default_claude_ai"}}`, ""},
		{"no profile", `null`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var profile map[string]any
			if err := json.Unmarshal([]byte(tc.profile), &profile); err != nil {
				t.Fatal(err)
			}
			subscription := claudeSubscription(profile)
			if tc.plan == "" {
				if subscription != nil {
					t.Fatalf("unexpected subscription: %#v", subscription)
				}
			} else if subscription == nil || subscription.Plan != tc.plan {
				t.Fatalf("subscription = %#v, want %s", subscription, tc.plan)
			}
		})
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
