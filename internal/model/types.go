package model

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// Envelope is the standard JSON envelope used by the C ABI plugin protocol.
type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *EnvelopeError  `json:"error,omitempty"`
}

// EnvelopeError describes an RPC error.
type EnvelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// HostConfig mirrors the host configuration summary passed to providers.
type HostConfig struct {
	AuthDir  string `json:"AuthDir,omitempty"`
	ProxyURL string `json:"ProxyURL,omitempty"`
}

// QuotaFetchRequest mirrors pluginapi.QuotaFetchRequest plus the callback id
// the host uses to bind host HTTP calls to this plugin instance.
type QuotaFetchRequest struct {
	AuthIndex      string            `json:"auth_index"`
	AuthID         string            `json:"auth_id"`
	Provider       string            `json:"provider"`
	StorageJSON    []byte            `json:"storage_json,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	Host           HostConfig        `json:"host,omitempty"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

// QuotaResetRequest mirrors pluginapi.QuotaResetRequest plus the callback id.
type QuotaResetRequest struct {
	AuthIndex      string            `json:"auth_index"`
	AuthID         string            `json:"auth_id"`
	Provider       string            `json:"provider"`
	StorageJSON    []byte            `json:"storage_json,omitempty"`
	Metadata       map[string]any    `json:"metadata,omitempty"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	Host           HostConfig        `json:"host,omitempty"`
	HostCallbackID string            `json:"host_callback_id,omitempty"`
}

// QuotaSubscription describes plan and tier information.
type QuotaSubscription struct {
	Plan     string `json:"plan,omitempty"`
	TierName string `json:"tierName,omitempty"`
	TierID   string `json:"tierId,omitempty"`
}

// QuotaGroup groups related quota buckets.
type QuotaGroup struct {
	DisplayName string        `json:"displayName,omitempty"`
	Buckets     []QuotaBucket `json:"buckets,omitempty"`
}

// QuotaBucket describes one quota window.
type QuotaBucket struct {
	Window            string  `json:"window,omitempty"`
	RemainingFraction float64 `json:"remainingFraction"`
	ResetTime         string  `json:"resetTime,omitempty"`
	Description       string  `json:"description,omitempty"`
}

// QuotaMetric is a provider-defined account summary value.
type QuotaMetric struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
	Format   string  `json:"format,omitempty"`
	Currency string  `json:"currency,omitempty"`
}

// QuotaFetchResponse is the normalized quota payload returned to CPA.
type QuotaFetchResponse struct {
	Subscription       *QuotaSubscription `json:"subscription,omitempty"`
	Summary            []QuotaMetric      `json:"summary,omitempty"`
	ServerTimeOffsetMs int64              `json:"serverTimeOffsetMs,omitempty"`
	Groups             []QuotaGroup       `json:"groups,omitempty"`
}

// QuotaDescribeResponse declares supported providers and capabilities.
type QuotaDescribeResponse struct {
	SupportedProviders []string `json:"supported_providers,omitempty"`
	DisplayName        string   `json:"display_name,omitempty"`
	SupportsReset      bool     `json:"supports_reset,omitempty"`
}

// QuotaResetResponse reports the result of a reset action.
type QuotaResetResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
}

// ManagementRequest describes a request forwarded to a plugin resource or
// management route.
type ManagementRequest struct {
	Method  string      `json:"Method"`
	Path    string      `json:"Path"`
	Headers http.Header `json:"Headers"`
	Query   url.Values  `json:"Query"`
	Body    []byte      `json:"Body"`
}

// ManagementResponse is a plugin resource or management response.
type ManagementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

// ManagementRegistrationResponse declares routes and resources.
type ManagementRegistrationResponse struct {
	Routes    []ManagementRoute `json:"routes,omitempty"`
	Resources []ResourceRoute   `json:"resources,omitempty"`
}

// ManagementRoute is an authenticated plugin management endpoint.
type ManagementRoute struct {
	Method      string `json:"Method"`
	Path        string `json:"Path"`
	Description string `json:"Description,omitempty"`
}

// ResourceRoute is a browser-navigable plugin page.
type ResourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description,omitempty"`
}
