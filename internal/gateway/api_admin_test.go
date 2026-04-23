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
	adminpkg "github.com/wweir/warden/internal/gateway/admin"
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

	adminpkg.NormalizePromptConfigJSON(cfg)

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

func TestGatewayRootRedirectsToAdminWhenAdminEnabled(t *testing.T) {
	cfg := &config.ConfigStruct{
		AdminPassword: config.SecretString("admin-secret"),
		Provider:      map[string]*config.ProviderConfig{},
		Route:         map[string]*config.RouteConfig{},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusFound, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/_admin/" {
		t.Fatalf("Location = %q, want /_admin/", got)
	}
}

func TestGatewayRootReturnsNotFoundWhenAdminDisabled(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{},
		Route:    map[string]*config.RouteConfig{},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	gw.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusNotFound, rec.Body.String())
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

	adminpkg.NormalizePromptConfigJSON(cfg)

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
		"route": map[string]any{
			"/openai": map[string]any{
				"api_keys": map[string]any{
					"cli": "client-secret",
				},
			},
		},
		"provider": map[string]any{
			"openai": map[string]any{
				"api_key": "provider-secret",
				"url":     "https://api.openai.com/v1",
			},
		},
	}

	adminpkg.NormalizeSecretConfigJSON(cfg)

	if got := cfg["admin_password"]; got != config.EncodeSecret("admin-secret") {
		t.Fatalf("admin_password = %v, want encoded secret", got)
	}

	apiKeys := cfg["route"].(map[string]any)["/openai"].(map[string]any)["api_keys"].(map[string]any)
	if got := apiKeys["cli"]; got != config.EncodeSecret("client-secret") {
		t.Fatalf("api_keys.cli = %v, want encoded secret", got)
	}

	provider := cfg["provider"].(map[string]any)["openai"].(map[string]any)
	if got := provider["api_key"]; got != config.EncodeSecret("provider-secret") {
		t.Fatalf("provider api_key = %v, want encoded secret", got)
	}
}

func TestNormalizeSecretConfigJSONDoesNotTouchHeaderAPIKeyValues(t *testing.T) {
	cfg := map[string]any{
		"provider": map[string]any{
			"openai": map[string]any{
				"api_key": "provider-secret",
				"headers": map[string]any{
					"api_key": "literal-header-secret",
				},
			},
		},
		"webhook": map[string]any{
			"audit": map[string]any{
				"headers": map[string]any{
					"api_key": "webhook-header-secret",
				},
			},
		},
	}

	adminpkg.NormalizeSecretConfigJSON(cfg)

	provider := cfg["provider"].(map[string]any)["openai"].(map[string]any)
	if got := provider["api_key"]; got != config.EncodeSecret("provider-secret") {
		t.Fatalf("provider api_key = %v, want encoded secret", got)
	}
	if got := provider["headers"].(map[string]any)["api_key"]; got != "literal-header-secret" {
		t.Fatalf("provider.headers.api_key = %v, want literal-header-secret", got)
	}
	webhook := cfg["webhook"].(map[string]any)["audit"].(map[string]any)
	if got := webhook["headers"].(map[string]any)["api_key"]; got != "webhook-header-secret" {
		t.Fatalf("webhook.headers.api_key = %v, want webhook-header-secret", got)
	}
}

func TestHandleAdminConfigPutPreservesMaskedSecretsWithoutDoubleEncoding(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr:          ":8080",
		AdminPassword: config.SecretString("admin-secret"),
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
				APIKeys: map[string]config.SecretString{
					"cli": "client-secret",
				},
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
	gw.adminHandler().HandleAdminConfigGet(getRec, getReq, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d, body=%q", getRec.Code, http.StatusOK, getRec.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/_admin/api/config", strings.NewReader(getRec.Body.String()))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	gw.adminHandler().HandleAdminConfigPut(putRec, putReq, nil)
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
	apiKeys := parsed["route"].(map[string]any)["/openai"].(map[string]any)["api_keys"].(map[string]any)
	if got := apiKeys["cli"]; got != config.EncodeSecret("client-secret") {
		t.Fatalf("api_keys.cli = %v, want %q", got, config.EncodeSecret("client-secret"))
	}
	providers := parsed["provider"].(map[string]any)
	providerCfg := providers["openai"].(map[string]any)
	if got := providerCfg["api_key"]; got != config.EncodeSecret("provider-secret") {
		t.Fatalf("provider api_key = %v, want %q", got, config.EncodeSecret("provider-secret"))
	}
}

func TestHandleAPIKeysCreatePersistsRouteAPIKey(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":8080",
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

	req := httptest.NewRequest(http.MethodPost, "/_admin/api/apikeys", strings.NewReader(`{"route":"/openai","name":"cli"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleAPIKeysCreate(rec, req, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if resp["route"] != "/openai" || resp["name"] != "cli" || resp["key"] == "" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if got := gw.cfg.Route["/openai"].APIKeys["cli"].Value(); got != resp["key"] {
		t.Fatalf("runtime key = %q, want %q", got, resp["key"])
	}

	saved, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(saved), "exactmodels:") {
		t.Fatalf("saved YAML should use json-tag keys, got %q", string(saved))
	}
	if strings.Contains(string(saved), "adminpassword:") {
		t.Fatalf("saved YAML should not contain yaml.v3 default field names, got %q", string(saved))
	}
	if !strings.Contains(string(saved), "exact_models:") || !strings.Contains(string(saved), "api_keys:") {
		t.Fatalf("saved YAML missing expected config keys: %q", string(saved))
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(saved, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	routeMap, ok := parsed["route"].(map[string]any)
	if !ok {
		t.Fatalf("saved route = %#v, want map", parsed["route"])
	}
	openaiRoute, ok := routeMap["/openai"].(map[string]any)
	if !ok {
		t.Fatalf("saved route /openai = %#v, want map", routeMap["/openai"])
	}
	apiKeys, ok := openaiRoute["api_keys"].(map[string]any)
	if !ok {
		t.Fatalf("saved api_keys = %#v, want map", openaiRoute["api_keys"])
	}
	if got := apiKeys["cli"]; got != config.EncodeSecret(resp["key"]) {
		t.Fatalf("saved key = %v, want %q", got, config.EncodeSecret(resp["key"]))
	}
}

func TestHandleAPIKeysCreateRejectsBlankName(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/_admin/api/apikeys", strings.NewReader(`{"route":" /openai ","name":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleAPIKeysCreate(rec, req, nil)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Fatalf("body = %q, want name validation error", rec.Body.String())
	}
	if len(gw.cfg.Route["/openai"].APIKeys) != 0 {
		t.Fatalf("route api keys = %#v, want empty", gw.cfg.Route["/openai"].APIKeys)
	}
}

func TestHandleAPIKeysCreateDoesNotMutateRuntimeWhenConfigWriteConflicts(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":8080",
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

	if err := os.WriteFile(file.Name(), []byte("changed: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/_admin/api/apikeys", strings.NewReader(`{"route":"/openai","name":"cli"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleAPIKeysCreate(rec, req, nil)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if gw.cfg.Route["/openai"].APIKeyCount() != 0 {
		t.Fatalf("runtime api keys = %#v, want unchanged empty set", gw.cfg.Route["/openai"].CloneAPIKeys())
	}
}

func TestHandleAPIKeysDeletePersistsRouteAPIKeyRemoval(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":8080",
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "https://api.openai.com/v1",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				APIKeys: map[string]config.SecretString{
					"cli": "client-secret",
				},
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

	req := httptest.NewRequest(http.MethodDelete, "/_admin/api/apikeys", strings.NewReader(`{"route":"/openai","name":"cli"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleAPIKeysDelete(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}
	if gw.cfg.Route["/openai"].APIKeyCount() != 0 {
		t.Fatalf("runtime api keys = %#v, want empty", gw.cfg.Route["/openai"].CloneAPIKeys())
	}

	saved, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	var parsed map[string]any
	if err := yaml.Unmarshal(saved, &parsed); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	openaiRoute := parsed["route"].(map[string]any)["/openai"].(map[string]any)
	apiKeys, exists := openaiRoute["api_keys"]
	if exists && apiKeys != nil {
		t.Fatalf("saved api_keys = %#v, want omitted or null after delete", apiKeys)
	}
}

func TestHandleAPIKeysDeleteDoesNotMutateRuntimeWhenConfigWriteConflicts(t *testing.T) {
	cfg := &config.ConfigStruct{
		Addr: ":8080",
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      "https://api.openai.com/v1",
				Protocol: "openai",
			},
		},
		Route: map[string]*config.RouteConfig{
			"/openai": {
				Protocol: config.RouteProtocolChat,
				APIKeys: map[string]config.SecretString{
					"cli": "client-secret",
				},
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

	if err := os.WriteFile(file.Name(), []byte("changed: true\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/_admin/api/apikeys", strings.NewReader(`{"route":"/openai","name":"cli"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleAPIKeysDelete(rec, req, nil)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if got := gw.cfg.Route["/openai"].CloneAPIKeys()["cli"].Value(); got != "client-secret" {
		t.Fatalf("runtime key = %q, want client-secret", got)
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
	gw.adminHandler().WriteStatusSSE(rec)

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
	gw.adminHandler().HandleProviderDetail(rec, req, nil)

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

func TestHandleProviderFormMetaIncludesCLIProxyDefaultsAndTemplates(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"codex": {
				URL:              "http://127.0.0.1:19001/v1",
				Protocol:         config.ProviderProtocolOpenAI,
				Backend:          config.ProviderBackendCLIProxy,
				BackendProvider:  "codex",
				ServiceProtocols: []string{config.RouteProtocolChat},
			},
		},
		Route: map[string]*config.RouteConfig{
			"/codex": {
				Protocol: config.RouteProtocolChat,
				WildcardModels: map[string]*config.WildcardRouteModelConfig{
					"*": {Providers: []string{"codex"}},
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodGet, "/_admin/api/providers/form-meta", nil)
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleProviderFormMeta(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		Presets []struct {
			ID                      string `json:"id"`
			Backend                 string `json:"backend"`
			BackendProvider         string `json:"backend_provider"`
			DefaultURL              string `json:"default_url"`
			ServiceProtocolTemplate string `json:"service_protocol_template"`
		} `json:"presets"`
		ServiceProtocolTemplates []struct {
			ID               string   `json:"id"`
			ServiceProtocols []string `json:"service_protocols"`
			AnthropicToChat  bool     `json:"anthropic_to_chat"`
		} `json:"service_protocol_templates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal provider form meta: %v", err)
	}

	var codexPreset *struct {
		ID                      string `json:"id"`
		Backend                 string `json:"backend"`
		BackendProvider         string `json:"backend_provider"`
		DefaultURL              string `json:"default_url"`
		ServiceProtocolTemplate string `json:"service_protocol_template"`
	}
	for i := range payload.Presets {
		if payload.Presets[i].ID == "cliproxy-codex" {
			codexPreset = &payload.Presets[i]
			break
		}
	}
	if codexPreset == nil {
		t.Fatal("cliproxy-codex preset not found")
	}
	if codexPreset.Backend != config.ProviderBackendCLIProxy {
		t.Fatalf("cliproxy-codex backend = %q, want %q", codexPreset.Backend, config.ProviderBackendCLIProxy)
	}
	if codexPreset.BackendProvider != "codex" {
		t.Fatalf("cliproxy-codex backend_provider = %q, want codex", codexPreset.BackendProvider)
	}
	if codexPreset.DefaultURL != "http://127.0.0.1:19001/v1" {
		t.Fatalf("cliproxy-codex default_url = %q, want existing cliproxy endpoint", codexPreset.DefaultURL)
	}
	if codexPreset.ServiceProtocolTemplate != "chat_only" {
		t.Fatalf("cliproxy-codex service_protocol_template = %q, want chat_only", codexPreset.ServiceProtocolTemplate)
	}

	foundAnthropicBridge := false
	for _, tpl := range payload.ServiceProtocolTemplates {
		if tpl.ID != "anthropic_bridge" {
			continue
		}
		foundAnthropicBridge = true
		if !sameStrings(tpl.ServiceProtocols, []string{config.RouteProtocolChat, config.RouteProtocolAnthropic}) {
			t.Fatalf("anthropic_bridge service_protocols = %v, want [chat anthropic]", tpl.ServiceProtocols)
		}
		if !tpl.AnthropicToChat {
			t.Fatal("anthropic_bridge anthropic_to_chat = false, want true")
		}
	}
	if !foundAnthropicBridge {
		t.Fatal("anthropic_bridge template not found")
	}

	var openAICompatiblePreset *struct {
		ID                      string `json:"id"`
		Backend                 string `json:"backend"`
		BackendProvider         string `json:"backend_provider"`
		DefaultURL              string `json:"default_url"`
		ServiceProtocolTemplate string `json:"service_protocol_template"`
	}
	for i := range payload.Presets {
		if payload.Presets[i].ID == "openai-compatible" {
			openAICompatiblePreset = &payload.Presets[i]
			break
		}
	}
	if openAICompatiblePreset == nil {
		t.Fatal("openai-compatible preset not found")
	}
	if openAICompatiblePreset.DefaultURL != "" {
		t.Fatalf("openai-compatible default_url = %q, want empty for user-supplied endpoint", openAICompatiblePreset.DefaultURL)
	}
}

func TestHandleProviderDetailIncludesEmbeddingsDisplayProtocol(t *testing.T) {
	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"embeddings": {
				URL:              "https://api.openai.com/v1",
				Protocol:         "openai",
				ServiceProtocols: []string{config.ServiceProtocolEmbeddings},
			},
			"chat": {
				URL:              "https://api.openai.com/v1",
				Protocol:         "openai",
				ServiceProtocols: []string{config.RouteProtocolChat},
			},
		},
		Route: map[string]*config.RouteConfig{
			"/proxy": {
				Protocol: config.RouteProtocolChat,
				ExactModels: map[string]*config.ExactRouteModelConfig{
					"hybrid": exactModel(config.RouteProtocolChat,
						&config.RouteUpstreamConfig{Provider: "embeddings", Model: "text-embedding-3-small"},
						&config.RouteUpstreamConfig{Provider: "chat", Model: "gpt-4o"},
					),
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	gw := NewGateway(cfg, "", "")
	t.Cleanup(gw.Close)

	req := httptest.NewRequest(http.MethodGet, "/_admin/api/providers/detail?name=embeddings", nil)
	rec := httptest.NewRecorder()
	gw.adminHandler().HandleProviderDetail(rec, req, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%q", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload struct {
		DisplayProtocols []string `json:"display_protocols"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal provider detail: %v", err)
	}
	if got := payload.DisplayProtocols; !sameStrings(got, []string{config.ServiceProtocolEmbeddings}) {
		t.Fatalf("display_protocols = %v, want [embeddings]", got)
	}
}

func TestNewGatewayRefreshesProviderModelsForExactRoutes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "gpt-4o", "object": "model", "owned_by": "openai"},
			},
		})
	}))
	defer upstream.Close()

	cfg := &config.ConfigStruct{
		Provider: map[string]*config.ProviderConfig{
			"openai": {
				URL:      upstream.URL,
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

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		models := gw.selector.ProviderModels("openai")
		if len(models) == 1 {
			status := gw.selector.ProviderDetail("openai")
			if status == nil {
				t.Fatal("ProviderDetail() = nil")
			}
			if status.ModelCount != 1 {
				t.Fatalf("ModelCount = %d, want 1", status.ModelCount)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("ProviderModels() did not refresh for exact route, got %d models", len(gw.selector.ProviderModels("openai")))
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
