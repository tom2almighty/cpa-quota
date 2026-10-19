package plugin

import (
	"encoding/json"
	"log"
	"sync"

	"cpa-quota/internal/handler"
	"cpa-quota/internal/hostapi"
	"cpa-quota/internal/model"
	"cpa-quota/internal/provider"
)

// Version is injected at build time.
var Version = "dev"

const (
	pluginID   = "cpa-quota"
	rpcSchema  = 6
	identifier = "cpa-quota"
)

// Instance is the singleton plugin instance.
type Instance struct {
	mu          sync.RWMutex
	htmlContent []byte
	hostCall    hostapi.Caller
}

var (
	instance *Instance
	once     sync.Once
)

// GetInstance returns the singleton plugin instance.
func GetInstance() *Instance {
	once.Do(func() { instance = &Instance{} })
	return instance
}

// SetEmbeddedHTML installs the embedded management page.
func (p *Instance) SetEmbeddedHTML(html []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.htmlContent = html
}

// SetHostCall installs the host RPC bridge used for upstream HTTP.
func (p *Instance) SetHostCall(call hostapi.Caller) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hostCall = call
}

// HandleCall routes incoming C ABI method calls.
func (p *Instance) HandleCall(method string, payload []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return p.handleRegister()
	case "plugin.quiesce", "plugin.shutdown":
		return okEnvelope(struct{}{})
	case "quota.identifier":
		return okEnvelope(map[string]string{"identifier": identifier})
	case "quota.describe":
		return p.handleDescribe()
	case "quota.fetch":
		return p.handleFetch(payload)
	case "quota.reset":
		return p.handleReset(payload)
	case "management.register":
		return p.handleManagementRegister()
	case "management.handle":
		return p.handleManagementHandle(payload)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

type registerResponse struct {
	SchemaVersion uint32           `json:"schema_version"`
	Metadata      metadataResponse `json:"metadata"`
	Capabilities  map[string]bool  `json:"capabilities"`
}

type metadataResponse struct {
	Name             string            `json:"Name"`
	Version          string            `json:"Version"`
	Author           string            `json:"Author"`
	GitHubRepository string            `json:"GitHubRepository"`
	Logo             string            `json:"Logo"`
	ConfigFields     []configFieldInfo `json:"ConfigFields"`
}

type configFieldInfo struct {
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	Description string `json:"Description"`
}

func (p *Instance) handleRegister() ([]byte, error) {
	return okEnvelope(registerResponse{
		SchemaVersion: rpcSchema,
		Metadata: metadataResponse{
			Name:             "CPA Quota",
			Version:          Version,
			Author:           "tom2almighty",
			GitHubRepository: "https://github.com/tom2almighty/cpa-quota",
			Logo:             "https://raw.githubusercontent.com/tom2almighty/cpa-quota/main/assets/logo.svg",
			ConfigFields:     []configFieldInfo{},
		},
		Capabilities: map[string]bool{
			"quota_provider": true,
			"management_api": true,
		},
	})
}

func (p *Instance) handleDescribe() ([]byte, error) {
	return okEnvelope(model.QuotaDescribeResponse{
		SupportedProviders: provider.SupportedProviders(),
		DisplayName:        "CPA Quota",
		SupportsReset:      true,
	})
}

func (p *Instance) handleFetch(payload []byte) ([]byte, error) {
	var request model.QuotaFetchRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return errorEnvelope("invalid_request", err.Error()), nil
	}
	providerImpl, ok := provider.Get(request.Provider)
	if !ok {
		return errorEnvelope("unsupported_provider", "unsupported provider: "+request.Provider), nil
	}
	ctx := provider.NewContext(request.AuthIndex, request.Provider, request.StorageJSON, request.Metadata, request.Attributes, p.client(request.HostCallbackID))
	response, err := providerImpl.Fetch(ctx)
	if err != nil {
		log.Printf("[%s] fetch %s quota failed: %v", pluginID, request.Provider, err)
		return errorEnvelope("fetch_failed", err.Error()), nil
	}
	return okEnvelope(response)
}

func (p *Instance) handleReset(payload []byte) ([]byte, error) {
	var request model.QuotaResetRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return errorEnvelope("invalid_request", err.Error()), nil
	}
	providerImpl, ok := provider.Get(request.Provider)
	if !ok {
		return errorEnvelope("unsupported_provider", "unsupported provider: "+request.Provider), nil
	}
	ctx := provider.NewContext(request.AuthIndex, request.Provider, request.StorageJSON, request.Metadata, request.Attributes, p.client(request.HostCallbackID))
	response, err := providerImpl.Reset(ctx)
	if err != nil {
		log.Printf("[%s] reset %s quota failed: %v", pluginID, request.Provider, err)
		return errorEnvelope("reset_failed", err.Error()), nil
	}
	return okEnvelope(response)
}

func (p *Instance) handleManagementRegister() ([]byte, error) {
	return okEnvelope(model.ManagementRegistrationResponse{
		Resources: []model.ResourceRoute{{
			Path:        "/quota",
			Menu:        "额度监控",
			Description: "各渠道账号额度与主动重置",
		}},
	})
}

func (p *Instance) handleManagementHandle(payload []byte) ([]byte, error) {
	var request model.ManagementRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		return errorEnvelope("invalid_request", err.Error()), nil
	}
	p.mu.RLock()
	html := p.htmlContent
	p.mu.RUnlock()
	return okEnvelope(handler.Handle(html, request))
}

func (p *Instance) client(callbackID string) *hostapi.Client {
	p.mu.RLock()
	call := p.hostCall
	p.mu.RUnlock()
	return hostapi.New(call, callbackID)
}

func okEnvelope(result any) ([]byte, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return errorEnvelope("marshal_error", err.Error()), nil
	}
	return json.Marshal(model.Envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(model.Envelope{
		OK:    false,
		Error: &model.EnvelopeError{Code: code, Message: message},
	})
	return raw
}
