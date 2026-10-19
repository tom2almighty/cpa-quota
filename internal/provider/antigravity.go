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
		"User-Agent":    "antigravity/hub/2.23.0 darwin/arm64",
	}
	var lastErr error
	for _, url := range antigravityQuotaURLs {
		resp, err := ctx.HTTP.Do(http.MethodPost, url, headers, body)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("antigravity quota returned %d: %s", resp.StatusCode, truncate(string(resp.Body), 200))
			if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 404 || resp.StatusCode == 429 {
				break
			}
			continue
		}
		payload, errDecode := decodeBody(resp)
		if errDecode != nil {
			lastErr = errDecode
			continue
		}
		return parseAntigravityQuota(payload), nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("antigravity quota is unavailable")
	}
	return model.QuotaFetchResponse{}, lastErr
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
