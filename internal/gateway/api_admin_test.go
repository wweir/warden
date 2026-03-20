package gateway

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
	"gopkg.in/yaml.v3"
)

func TestNormalizePromptConfigJSONDropsDisabledFalseFlagsWithoutPrompt(t *testing.T) {
	cfg := map[string]any{
		"route": map[string]any{
			"/openai": map[string]any{
				"exact_models": map[string]any{
					"gpt-5.3-codex": map[string]any{
						"prompt_enabled": false,
						"upstreams": []any{
							map[string]any{
								"provider": "openai",
								"model":    "gpt-5.3-codex",
							},
						},
					},
				},
				"wildcard_models": map[string]any{
					"gpt-*": map[string]any{
						"prompt_enabled": false,
						"system_prompt":  "",
						"providers":      []any{"openai"},
					},
				},
			},
		},
	}

	normalizePromptConfigJSON(cfg)

	route := cfg["route"].(map[string]any)["/openai"].(map[string]any)
	exact := route["exact_models"].(map[string]any)["gpt-5.3-codex"].(map[string]any)
	if _, exists := exact["prompt_enabled"]; exists {
		t.Fatal("exact model prompt_enabled should be dropped when disabled and prompt is empty")
	}

	wildcard := route["wildcard_models"].(map[string]any)["gpt-*"].(map[string]any)
	if _, exists := wildcard["prompt_enabled"]; exists {
		t.Fatal("wildcard model prompt_enabled should be dropped when disabled and prompt is empty")
	}
}

func TestNormalizePromptConfigJSONKeepsEnabledPromptFlags(t *testing.T) {
	cfg := map[string]any{
		"route": map[string]any{
			"/openai": map[string]any{
				"exact_models": map[string]any{
					"gpt-5.3-codex": map[string]any{
						"prompt_enabled": true,
						"system_prompt":  "You are precise.",
						"upstreams": []any{
							map[string]any{
								"provider": "openai",
								"model":    "gpt-5.3-codex",
							},
						},
					},
				},
			},
		},
	}

	normalizePromptConfigJSON(cfg)

	exact := cfg["route"].(map[string]any)["/openai"].(map[string]any)["exact_models"].(map[string]any)["gpt-5.3-codex"].(map[string]any)
	if promptEnabled, ok := exact["prompt_enabled"].(bool); !ok || !promptEnabled {
		t.Fatalf("prompt_enabled = %v, want true", exact["prompt_enabled"])
	}
	if systemPrompt := exact["system_prompt"]; systemPrompt != "You are precise." {
		t.Fatalf("system_prompt = %v, want preserved prompt", systemPrompt)
	}
}

func TestNormalizeSecretConfigJSONEncodesSecretFields(t *testing.T) {
	cfg := map[string]any{
		"admin_password": "admin-secret",
		"api_keys": map[string]any{
			"cli": "client-secret",
		},
		"provider": map[string]any{
			"openai": map[string]any{
				"api_key": "provider-secret",
				"url":     "https://api.openai.com/v1",
			},
		},
	}

	normalizeSecretConfigJSON(cfg)

	if got := cfg["admin_password"]; got != config.EncodeSecret("admin-secret") {
		t.Fatalf("admin_password = %v, want encoded secret", got)
	}

	apiKeys := cfg["api_keys"].(map[string]any)
	if got := apiKeys["cli"]; got != config.EncodeSecret("client-secret") {
		t.Fatalf("api_keys.cli = %v, want encoded secret", got)
	}

	provider := cfg["provider"].(map[string]any)["openai"].(map[string]any)
	if got := provider["api_key"]; got != config.EncodeSecret("provider-secret") {
		t.Fatalf("provider api_key = %v, want encoded secret", got)
	}
}

func TestHandleAdminConfigPutPreservesMaskedSecretsWithoutDoubleEncoding(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr:          ":8080",
		AdminPassword: config.SecretString("admin-secret"),
		APIKeys: map[string]config.SecretString{
			"cli": "client-secret",
		},
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "https://api.openai.com/v1",
				Protocol: "openai",
				APIKey:   config.SecretString("provider-secret"),
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}

	file, err := os.CreateTemp(t.TempDir(), "warden-config-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if _, err := file.Write(yamlData); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	hash := sha256.Sum256(yamlData)
	gw := NewGateway(cfg, file.Name(), "")
	gw.configHash = fmt.Sprintf("%x", hash)
	t.Cleanup(gw.Close)

	getReq := httptest.NewRequest(http.MethodGet, "/_admin/api/config", nil)
	getRec := httptest.NewRecorder()
	gw.handleAdminConfigGet(getRec, getReq, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d, body=%q", getRec.Code, http.StatusOK, getRec.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/_admin/api/config", strings.NewReader(getRec.Body.String()))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	gw.handleAdminConfigPut(putRec, putReq, nil)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d, body=%q", putRec.Code, http.StatusOK, putRec.Body.String())
	}

	saved, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(saved, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := parsed["admin_password"]; got != config.EncodeSecret("admin-secret") {
		t.Fatalf("admin_password = %v, want %q", got, config.EncodeSecret("admin-secret"))
	}
	apiKeys := parsed["api_keys"].(map[string]any)
	if got := apiKeys["cli"]; got != config.EncodeSecret("client-secret") {
		t.Fatalf("api_keys.cli = %v, want %q", got, config.EncodeSecret("client-secret"))
	}
	providers := parsed["provider"].(map[string]any)
	providerCfg := providers["openai"].(map[string]any)
	if got := providerCfg["api_key"]; got != config.EncodeSecret("provider-secret") {
		t.Fatalf("provider api_key = %v, want %q", got, config.EncodeSecret("provider-secret"))
	}
}

func TestWriteStatusSSEReturnsConfiguredAndDisplayProtocolsSeparately(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "https://api.openai.com/v1",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	probe := &sel.ProtocolProbe{
		CheckedAt: time.Unix(1, 0),
		Status:    "ok",
		Source:    "test",
	}
	if ok := gw.selector.SetDisplayProtocols("openai", []string{config.RouteProtocolChat}, probe); !ok {
		t.Fatal("SetDisplayProtocols() returned false")
	}

	rec := httptest.NewRecorder()
	gw.writeStatusSSE(rec)

	body := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(body, "data: ") {
		t.Fatalf("unexpected SSE payload %q", body)
	}

	var payload struct {
		Providers []struct {
			Name                string   `json:"name"`
			SupportedProtocols  []string `json:"supported_protocols"`
			ConfiguredProtocols []string `json:"configured_protocols"`
			DisplayProtocols    []string `json:"display_protocols"`
		} `json:"providers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, "data: ")), &payload); err != nil {
		t.Fatalf("unmarshal status payload: %v", err)
	}
	if len(payload.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(payload.Providers))
	}

	wantSupported := []string{
		config.RouteProtocolChat,
		config.RouteProtocolResponsesStateless,
		config.RouteProtocolResponsesStateful,
	}
	if got := payload.Providers[0].SupportedProtocols; !sameStrings(got, wantSupported) {
		t.Fatalf("supported_protocols = %v, want %v", got, wantSupported)
	}
	if got := payload.Providers[0].ConfiguredProtocols; !sameStrings(got, wantSupported) {
		t.Fatalf("configured_protocols = %v, want %v", got, wantSupported)
	}
	if got := payload.Providers[0].DisplayProtocols; !sameStrings(got, []string{config.RouteProtocolChat}) {
		t.Fatalf("display_protocols = %v, want [chat]", got)
	}
}

func TestHandleProviderDetailReturnsConfiguredAndDisplayProtocolsSeparately(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "https://api.openai.com/v1",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"gpt-4o": exactModel(config.RouteProtocolChat, &config.RouteUpstreamConfig{Provider: "openai", Model: "gpt-4o"}),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	probe := &sel.ProtocolProbe{
		CheckedAt: time.Unix(2, 0),
		Status:    "ok",
		Source:    "test",
	}
	if ok := gw.selector.SetDisplayProtocols("openai", []string{config.RouteProtocolChat}, probe); !ok {
		t.Fatal("SetDisplayProtocols() returned false")
	}

	req := httptest.NewRequest(http.MethodGet, "/_admin/api/providers/detail?name=openai", nil)
	rec := httptest.NewRecorder()
	gw.handleProviderDetail(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		SupportedProtocols  []string `json:"supported_protocols"`
		ConfiguredProtocols []string `json:"configured_protocols"`
		DisplayProtocols    []string `json:"display_protocols"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal provider detail: %v", err)
	}

	wantSupported := []string{
		config.RouteProtocolChat,
		config.RouteProtocolResponsesStateless,
		config.RouteProtocolResponsesStateful,
	}
	if got := payload.SupportedProtocols; !sameStrings(got, wantSupported) {
		t.Fatalf("supported_protocols = %v, want %v", got, wantSupported)
	}
	if got := payload.ConfiguredProtocols; !sameStrings(got, wantSupported) {
		t.Fatalf("configured_protocols = %v, want %v", got, wantSupported)
	}
	if got := payload.DisplayProtocols; !sameStrings(got, []string{config.RouteProtocolChat}) {
		t.Fatalf("display_protocols = %v, want [chat]", got)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
