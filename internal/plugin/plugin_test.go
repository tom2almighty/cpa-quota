package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"cpa-quota/internal/model"
)

func decodeResult[T any](t *testing.T, raw []byte) (T, model.Envelope) {
	t.Helper()
	var envelope model.Envelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var result T
	if envelope.OK {
		if err := json.Unmarshal(envelope.Result, &result); err != nil {
			t.Fatalf("decode result: %v", err)
		}
	}
	return result, envelope
}

func TestRegistration(t *testing.T) {
	raw, err := GetInstance().HandleCall("plugin.register", nil)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	response, envelope := decodeResult[registerResponse](t, raw)
	if !envelope.OK {
		t.Fatalf("envelope = %#v", envelope)
	}
	if !response.Capabilities["quota_provider"] || !response.Capabilities["management_api"] {
		t.Fatalf("capabilities = %#v", response.Capabilities)
	}
	if response.SchemaVersion != rpcSchema {
		t.Fatalf("schema = %d", response.SchemaVersion)
	}
}

func TestIdentifierAndDescribe(t *testing.T) {
	raw, _ := GetInstance().HandleCall("quota.identifier", nil)
	identifier, envelope := decodeResult[map[string]string](t, raw)
	if !envelope.OK || identifier["identifier"] != "cpa-quota" {
		t.Fatalf("identifier = %#v", identifier)
	}

	raw, _ = GetInstance().HandleCall("quota.describe", nil)
	describe, envelope := decodeResult[model.QuotaDescribeResponse](t, raw)
	if !envelope.OK || len(describe.SupportedProviders) == 0 || !describe.SupportsReset {
		t.Fatalf("describe = %#v", describe)
	}
}

func TestManagementResource(t *testing.T) {
	GetInstance().SetEmbeddedHTML([]byte("<!doctype html><title>quota</title>"))
	raw, _ := GetInstance().HandleCall("management.register", nil)
	registration, envelope := decodeResult[model.ManagementRegistrationResponse](t, raw)
	if !envelope.OK || len(registration.Resources) != 1 || registration.Resources[0].Path != "/quota" {
		t.Fatalf("registration = %#v", registration)
	}

	request, _ := json.Marshal(model.ManagementRequest{Method: "GET", Path: "/quota"})
	raw, _ = GetInstance().HandleCall("management.handle", request)
	response, envelope := decodeResult[model.ManagementResponse](t, raw)
	if !envelope.OK || response.StatusCode != 200 || !strings.Contains(string(response.Body), "quota") {
		t.Fatalf("response = %#v", response)
	}
}

func TestUnsupportedProvider(t *testing.T) {
	request, _ := json.Marshal(model.QuotaFetchRequest{Provider: "unknown"})
	raw, _ := GetInstance().HandleCall("quota.fetch", request)
	_, envelope := decodeResult[any](t, raw)
	if envelope.OK || envelope.Error == nil || envelope.Error.Code != "unsupported_provider" {
		t.Fatalf("envelope = %#v", envelope)
	}
}
