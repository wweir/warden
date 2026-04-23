package config

import "strings"

const (
	ProviderProtocolOpenAI    = "openai"
	ProviderProtocolAnthropic = "anthropic"
	ProviderProtocolQwen      = "qwen"
	ProviderProtocolCopilot   = "copilot"
)

const (
	ProviderBackendCLIProxy = "cliproxy"
)

func normalizeProviderProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func normalizeProviderBackend(backend string) string {
	return strings.ToLower(strings.TrimSpace(backend))
}

func normalizeRouteProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func normalizeProviderAdapterProtocol(protocol string) string {
	return normalizeProviderProtocol(protocol)
}
