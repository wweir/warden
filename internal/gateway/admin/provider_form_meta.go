package admin

import (
	"encoding/json"
	"net/http"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
)

const defaultCLIProxyProviderURL = "http://127.0.0.1:18741/v1"

type providerFormMetaResponse struct {
	Presets                  []providerFormPresetMeta                  `json:"presets"`
	ServiceProtocolTemplates []providerFormServiceProtocolTemplateMeta `json:"service_protocol_templates"`
}

type providerFormPresetMeta struct {
	ID                      string   `json:"id"`
	Title                   string   `json:"title"`
	Summary                 string   `json:"summary"`
	Family                  string   `json:"family"`
	Backend                 string   `json:"backend,omitempty"`
	BackendProvider         string   `json:"backend_provider,omitempty"`
	AuthMode                string   `json:"auth_mode"`
	DefaultURL              string   `json:"default_url,omitempty"`
	DefaultConfigDir        string   `json:"default_config_dir,omitempty"`
	ServiceProtocolTemplate string   `json:"service_protocol_template"`
	RecommendedModels       []string `json:"recommended_models,omitempty"`
}

type providerFormServiceProtocolTemplateMeta struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Summary          string   `json:"summary"`
	Families         []string `json:"families,omitempty"`
	Backends         []string `json:"backends,omitempty"`
	ServiceProtocols []string `json:"service_protocols"`
	AnthropicToChat  bool     `json:"anthropic_to_chat,omitempty"`
}

func (h *Handler) HandleProviderFormMeta(w http.ResponseWriter, _ *http.Request, _ httprouter.Params) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(buildProviderFormMeta(h.cfg))
}

func buildProviderFormMeta(cfg *config.ConfigStruct) providerFormMetaResponse {
	cliproxyURL := inferCLIProxyProviderURL(cfg)
	return providerFormMetaResponse{
		Presets: []providerFormPresetMeta{
			{
				ID:                      "anthropic-official",
				Title:                   "Anthropic-compatible",
				Summary:                 "Anthropic-compatible /v1 endpoint with chat + anthropic defaults.",
				Family:                  config.ProviderProtocolAnthropic,
				AuthMode:                "api_key",
				DefaultURL:              "https://api.anthropic.com/v1",
				ServiceProtocolTemplate: "adapter_defaults",
			},
			{
				ID:                      "openai-compatible",
				Title:                   "OpenAI-compatible",
				Summary:                 "OpenAI-compatible upstream for OpenAI official, custom vendors, and self-hosted gateways.",
				Family:                  config.ProviderProtocolOpenAI,
				AuthMode:                "api_key",
				ServiceProtocolTemplate: "adapter_defaults",
			},
			{
				ID:                      "ollama-chat",
				Title:                   "Ollama / Chat Only",
				Summary:                 "OpenAI-compatible Ollama or local chat-only endpoint with explicit chat narrowing.",
				Family:                  config.ProviderProtocolOpenAI,
				AuthMode:                "api_key",
				DefaultURL:              "http://127.0.0.1:11434/v1",
				ServiceProtocolTemplate: "chat_only",
			},
			{
				ID:                      "cliproxy-codex",
				Title:                   "CLIProxy Codex",
				Summary:                 "Local or embedded cliproxy /v1 endpoint backed by Codex. Defaults to the verified chat surface.",
				Family:                  config.ProviderProtocolOpenAI,
				Backend:                 config.ProviderBackendCLIProxy,
				BackendProvider:         "codex",
				AuthMode:                "none",
				DefaultURL:              cliproxyURL,
				ServiceProtocolTemplate: "chat_only",
			},
			{
				ID:                      "cliproxy-claude",
				Title:                   "CLIProxy Claude",
				Summary:                 "Local or embedded cliproxy /v1 endpoint backed by Claude CLI credentials. Defaults to chat through the OpenAI-compatible surface.",
				Family:                  config.ProviderProtocolOpenAI,
				Backend:                 config.ProviderBackendCLIProxy,
				BackendProvider:         "claude",
				AuthMode:                "none",
				DefaultURL:              cliproxyURL,
				ServiceProtocolTemplate: "chat_only",
			},
			{
				ID:                      "cliproxy-gemini",
				Title:                   "CLIProxy Gemini",
				Summary:                 "Local or embedded cliproxy /v1 endpoint backed by Gemini CLI credentials. Defaults to chat through the OpenAI-compatible surface.",
				Family:                  config.ProviderProtocolOpenAI,
				Backend:                 config.ProviderBackendCLIProxy,
				BackendProvider:         "gemini",
				AuthMode:                "none",
				DefaultURL:              cliproxyURL,
				ServiceProtocolTemplate: "chat_only",
			},
			{
				ID:                      "copilot-cli",
				Title:                   "GitHub Copilot",
				Summary:                 "Copilot adapter using local GitHub credentials from config_dir.",
				Family:                  config.ProviderProtocolCopilot,
				AuthMode:                "config_dir",
				DefaultURL:              "https://api.githubcopilot.com",
				DefaultConfigDir:        "~/.config/github-copilot",
				ServiceProtocolTemplate: "adapter_defaults",
			},
		},
		ServiceProtocolTemplates: []providerFormServiceProtocolTemplateMeta{
			{
				ID:               "adapter_defaults",
				Title:            "Adapter Defaults",
				Summary:          "Use the adapter default capability set for this family.",
				Families:         []string{config.ProviderProtocolOpenAI, config.ProviderProtocolAnthropic, config.ProviderProtocolCopilot},
				Backends:         []string{""},
				ServiceProtocols: []string{},
			},
			{
				ID:               "chat_only",
				Title:            "Chat Only",
				Summary:          "Expose chat only. Recommended for Ollama and cliproxy-backed providers unless Responses has been verified for this endpoint.",
				Families:         []string{config.ProviderProtocolOpenAI, config.ProviderProtocolAnthropic, config.ProviderProtocolCopilot},
				ServiceProtocols: []string{config.RouteProtocolChat},
			},
			{
				ID:               "chat_embeddings",
				Title:            "Chat + Embeddings",
				Summary:          "Expose chat and embeddings, without Responses surfaces.",
				Families:         []string{config.ProviderProtocolOpenAI},
				Backends:         []string{""},
				ServiceProtocols: []string{config.RouteProtocolChat, config.ServiceProtocolEmbeddings},
			},
			{
				ID:               "chat_responses_embeddings",
				Title:            "Chat + Responses + Embeddings",
				Summary:          "Expose chat, Responses stateless/stateful, and embeddings.",
				Families:         []string{config.ProviderProtocolOpenAI},
				Backends:         []string{""},
				ServiceProtocols: []string{config.RouteProtocolChat, config.RouteProtocolResponsesStateless, config.RouteProtocolResponsesStateful, config.ServiceProtocolEmbeddings},
			},
			{
				ID:               "anthropic_bridge",
				Title:            "Anthropic Bridge",
				Summary:          "Expose Anthropic /messages through an OpenAI provider using anthropic_to_chat.",
				Families:         []string{config.ProviderProtocolOpenAI},
				Backends:         []string{""},
				ServiceProtocols: []string{config.RouteProtocolChat, config.RouteProtocolAnthropic},
				AnthropicToChat:  true,
			},
		},
	}
}

func inferCLIProxyProviderURL(cfg *config.ConfigStruct) string {
	if cfg != nil {
		for _, prov := range cfg.Provider {
			if prov == nil || prov.Backend != config.ProviderBackendCLIProxy {
				continue
			}
			if prov.URL != "" {
				return prov.URL
			}
		}
	}
	return defaultCLIProxyProviderURL
}
