package provider

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"cpa-quota/internal/hostapi"
	"cpa-quota/internal/model"
)

// Context carries one credential's request context.
type Context struct {
	AuthIndex  string
	Provider   string
	Auth       map[string]any
	Metadata   map[string]any
	Attributes map[string]string
	HTTP       *hostapi.Client
	Now        time.Time
}

// Provider fetches and optionally resets one provider's quota.
type Provider interface {
	Fetch(*Context) (model.QuotaFetchResponse, error)
	Reset(*Context) (model.QuotaResetResponse, error)
}

var registry = map[string]Provider{}

func register(names []string, provider Provider) {
	for _, name := range names {
		registry[name] = provider
	}
}

// Get resolves a provider by its normalized name.
func Get(name string) (Provider, bool) {
	provider, ok := registry[normalizeProvider(name)]
	return provider, ok
}

// SupportedProviders lists the provider keys this plugin handles.
func SupportedProviders() []string {
	return []string{"claude", "codex", "antigravity", "kimi", "kimi-ai", "xai", "devin", "meta"}
}

func normalizeProvider(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "anthropic":
		return "claude"
	case "kimi.ai":
		return "kimi-ai"
	case "x-ai", "grok":
		return "xai"
	case "muse":
		return "meta"
	default:
		return name
	}
}

// NewContext builds a provider context from a fetch or reset request.
func NewContext(authIndex, provider string, storage []byte, metadata map[string]any, attributes map[string]string, http *hostapi.Client) *Context {
	auth := parseJSONMap(storage)
	if auth == nil {
		auth = metadata
	}
	if metadata == nil {
		metadata = auth
	}
	return &Context{
		AuthIndex:  authIndex,
		Provider:   normalizeProvider(provider),
		Auth:       auth,
		Metadata:   metadata,
		Attributes: attributes,
		HTTP:       http,
		Now:        time.Now(),
	}
}

func parseJSONMap(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

// decodeBody parses a JSON response body into a map.
func decodeBody(resp *hostapi.Response) (map[string]any, error) {
	if resp == nil {
		return nil, fmt.Errorf("empty upstream response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned %d: %s", resp.StatusCode, truncate(string(resp.Body), 200))
	}
	var out map[string]any
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, fmt.Errorf("upstream returned invalid JSON: %w", err)
	}
	return out, nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// lookupString searches a nested JSON value for the first matching key.
// JWT strings are decoded because several providers keep identity claims there.
func lookupString(value any, keys ...string) string {
	return lookupStringDepth(value, keys, 0)
}

func lookupStringDepth(value any, keys []string, depth int) string {
	if depth > 3 || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		if parts := strings.Split(typed, "."); len(parts) == 3 {
			if claims := decodeJWT(typed); claims != nil {
				if found := lookupStringDepth(claims, keys, depth+1); found != "" {
					return found
				}
			}
		}
		return ""
	case map[string]any:
		for _, key := range keys {
			if raw, ok := typed[key]; ok {
				if text := asString(raw); text != "" {
					return text
				}
			}
		}
		for _, nested := range []string{"id_token", "oauth", "user", "installed", "web", "https://api.openai.com/auth"} {
			if child, ok := typed[nested]; ok {
				if found := lookupStringDepth(child, keys, depth+1); found != "" {
					return found
				}
			}
		}
	}
	return ""
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%v", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

func mapNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case string:
		var number float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &number); err == nil {
			return number, true
		}
	case map[string]any:
		if raw, ok := typed["val"]; ok {
			return mapNumber(raw)
		}
		if raw, ok := typed["value"]; ok {
			return mapNumber(raw)
		}
	}
	return 0, false
}

func getMap(value any, key string) map[string]any {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	child, _ := record[key].(map[string]any)
	return child
}

func getArray(value any, key string) []any {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	array, _ := record[key].([]any)
	return array
}

func clampFraction(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func remainingFromUsedPercent(percent float64) float64 {
	return clampFraction(1 - percent/100)
}

func isoFromUnix(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(int64(seconds), 0).UTC().Format(time.RFC3339)
}

func isoFromUnixMillis(millis float64) string {
	if millis <= 0 {
		return ""
	}
	return time.UnixMilli(int64(millis)).UTC().Format(time.RFC3339)
}

func isoFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return ""
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.UTC().Format(time.RFC3339)
		}
		return ""
	case float64:
		if typed < 1e12 {
			return isoFromUnix(typed)
		}
		return isoFromUnixMillis(typed)
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return ""
		}
		return isoFromAny(number)
	default:
		return ""
	}
}

// windowKey maps a duration to a stable key understood by the frontend.
func windowKey(seconds float64) string {
	switch {
	case seconds <= 0:
		return ""
	case seconds <= 6*3600:
		return "five_hour"
	case seconds >= 6.5*86400 && seconds <= 7.5*86400:
		return "weekly"
	case seconds >= 27*86400 && seconds <= 32*86400:
		return "monthly"
	case seconds >= 86400:
		return fmt.Sprintf("days_%d", int(math.Round(seconds/86400)))
	default:
		return fmt.Sprintf("hours_%d", int(math.Round(seconds/3600)))
	}
}

func providerLabel(name string) string {
	switch name {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "antigravity":
		return "Antigravity"
	case "kimi", "kimi-ai":
		return "Kimi"
	case "xai":
		return "xAI"
	case "devin":
		return "Devin"
	case "meta":
		return "Meta"
	default:
		return name
	}
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	// RFC 4122 version 4 variant.
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(buf[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}
