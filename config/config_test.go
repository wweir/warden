package config

import (
	"encoding/json"
	"testing"
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
			name:  "empty patch returns original",
			body:  `{"model":"glm-5","thinking":{"type":"adaptive"}}`,
			patch: []RequestPatchOp{},
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
