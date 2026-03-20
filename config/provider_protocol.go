package config

import "strings"

const (
	ProviderProtocolOpenAI    = "openai"
	ProviderProtocolAnthropic = "anthropic"
	ProviderProtocolOllama    = "ollama"
	ProviderProtocolQwen      = "qwen"
	ProviderProtocolCopilot   = "copilot"
)

func normalizeProviderProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func normalizeRouteProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}
