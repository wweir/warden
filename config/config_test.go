package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestApplyRequestPatch(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		patch   []RequestPatchOp
		want    string // expected JSON key presence; use json.Unmarshal to compare
		wantKey string // key to check existence
		wantHas bool   // whether the key should be present
	}{
		{
			name: "remove existing field",
			body: `{"model":"glm-5","thinking":{"type":"adaptive"},"stream":true}`,
			patch: []RequestPatchOp{
				{Op: "remove", Path: "/thinking"},
			},
			wantKey: "thinking",
			wantHas: false,
		},
		{
			name: "remove missing field silently",
			body: `{"model":"glm-5"}`,
			patch: []RequestPatchOp{
				{Op: "remove", Path: "/thinking"},
			},
			wantKey: "model",
			wantHas: true,
		},
		{
			name:    "empty patch returns original",
			body:    `{"model":"glm-5","thinking":{"type":"adaptive"}}`,
			patch:   []RequestPatchOp{},
			wantKey: "thinking",
			wantHas: true,
		},
		{
			name: "replace field value",
			body: `{"model":"glm-5","max_tokens":32000}`,
			patch: []RequestPatchOp{
				{Op: "replace", Path: "/max_tokens", Value: json.RawMessage(`8192`)},
			},
			wantKey: "max_tokens",
			wantHas: true,
		},
		{
			name: "add new field",
			body: `{"model":"glm-5"}`,
			patch: []RequestPatchOp{
				{Op: "add", Path: "/stream", Value: json.RawMessage(`false`)},
			},
			wantKey: "stream",
			wantHas: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &ProviderConfig{RequestPatch: tt.patch}
			result := prov.ApplyRequestPatch([]byte(tt.body))

			var got map[string]json.RawMessage
			if err := json.Unmarshal(result, &got); err != nil {
				t.Fatalf("result is not valid JSON: %v, body: %s", err, result)
			}

			_, has := got[tt.wantKey]
			if has != tt.wantHas {
				t.Errorf("key %q present=%v, want %v; result: %s", tt.wantKey, has, tt.wantHas, result)
			}
		})
	}
}

func TestApplyRequestPatch_InvalidBody(t *testing.T) {
	prov := &ProviderConfig{RequestPatch: []RequestPatchOp{
		{Op: "remove", Path: "/thinking"},
	}}
	body := []byte("not-json")
	result := prov.ApplyRequestPatch(body)
	if string(result) != string(body) {
		t.Errorf("expected original body returned for invalid JSON, got %s", result)
	}
}

func TestApplyRequestPatch_ReplaceValue(t *testing.T) {
	prov := &ProviderConfig{RequestPatch: []RequestPatchOp{
		{Op: "replace", Path: "/max_tokens", Value: json.RawMessage(`8192`)},
	}}
	body := []byte(`{"model":"glm-5","max_tokens":32000}`)
	result := prov.ApplyRequestPatch(body)

	var got map[string]json.RawMessage
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	var maxTokens int
	json.Unmarshal(got["max_tokens"], &maxTokens)
	if maxTokens != 8192 {
		t.Errorf("max_tokens = %d, want 8192", maxTokens)
	}
}

func TestApplyRequestPatch_MultipleOps(t *testing.T) {
	prov := &ProviderConfig{RequestPatch: []RequestPatchOp{
		{Op: "remove", Path: "/thinking"},
		{Op: "remove", Path: "/metadata"},
	}}
	body := []byte(`{"model":"glm-5","thinking":{"type":"adaptive"},"metadata":{"user_id":"abc"}}`)
	result := prov.ApplyRequestPatch(body)

	var got map[string]json.RawMessage
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, has := got["thinking"]; has {
		t.Error("thinking should be removed")
	}
	if _, has := got["metadata"]; has {
		t.Error("metadata should be removed")
	}
	if _, has := got["model"]; !has {
		t.Error("model should remain")
	}
}

// TestApplyRequestPatch_YAMLObjectValue tests that value fields passed as map[string]any
// (as happens when YAML config uses object syntax like value: {type: "enabled"})
// are correctly serialized and applied as JSON objects.
func TestApplyRequestPatch_YAMLObjectValue(t *testing.T) {
	// Simulate feconf/mapstructure converting YAML object to map[string]any
	prov := &ProviderConfig{RequestPatch: []RequestPatchOp{
		{Op: "add", Path: "/thinking", Value: map[string]any{"type": "enabled"}},
	}}
	body := []byte(`{"model":"glm-5","thinking":{"type":"adaptive"}}`)
	result := prov.ApplyRequestPatch(body)

	var got map[string]json.RawMessage
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("invalid JSON: %v, body: %s", err, result)
	}
	var thinking map[string]string
	if err := json.Unmarshal(got["thinking"], &thinking); err != nil {
		t.Fatalf("thinking is not a JSON object: %s", got["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %q, want enabled; result: %s", thinking["type"], result)
	}
}

func TestProviderConfig_HTTPClient_Caching(t *testing.T) {
	prov := &ProviderConfig{
		Name:    "test",
		URL:     "http://localhost:8080",
		Timeout: "30s",
	}

	// Get client for default timeout
	client1 := prov.HTTPClient(0)
	client2 := prov.HTTPClient(0)

	// Should return same cached instance
	if client1 != client2 {
		t.Error("HTTPClient should return cached instance for same timeout")
	}

	// Get client for different timeout
	client3 := prov.HTTPClient(60 * time.Second)
	if client1 == client3 {
		t.Error("HTTPClient should return different instance for different timeout")
	}

	// Get client for same custom timeout again
	client4 := prov.HTTPClient(60 * time.Second)
	if client3 != client4 {
		t.Error("HTTPClient should cache instance for custom timeout")
	}
}

func TestValidateToolHookHTTPType(t *testing.T) {
	cfg := &ConfigStruct{
		Webhook: map[string]*WebhookConfig{
			"audit": {URL: "http://127.0.0.1:8080/hook"},
		},
		ToolHooks: []*HookRuleConfig{
			{
				Match: "*",
				Hook: HookConfig{
					Type:    "http",
					When:    "pre",
					Timeout: "3s",
					Webhook: "audit",
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if cfg.ToolHooks[0].Hook.WebhookCfg == nil {
		t.Fatal("expected webhook config to be resolved")
	}
}

func TestValidateToolHookHTTPTypeUnknownWebhook(t *testing.T) {
	cfg := &ConfigStruct{
		Webhook: map[string]*WebhookConfig{
			"audit": {URL: "http://127.0.0.1:8080/hook"},
		},
		ToolHooks: []*HookRuleConfig{
			{
				Match: "*",
				Hook: HookConfig{
					Type:    "http",
					When:    "pre",
					Timeout: "3s",
					Webhook: "missing",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "unknown webhook") {
		t.Fatalf("expected unknown webhook error, got %v", err)
	}
}

func TestValidateToolHookHTTPTypeMissingWebhook(t *testing.T) {
	cfg := &ConfigStruct{
		ToolHooks: []*HookRuleConfig{
			{
				Match: "*",
				Hook: HookConfig{
					Type:    "http",
					When:    "pre",
					Timeout: "3s",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "webhook is required") {
		t.Fatalf("expected webhook required error, got %v", err)
	}
}
