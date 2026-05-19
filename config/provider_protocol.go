package config

import "strings"

const (
	ProviderFormatOpenAI    = "openai"
	ProviderFormatAnthropic = "anthropic"
	ProviderFormatCopilot   = "copilot"
)

const (
	ProviderBackendCLIProxy = "cliproxy"
)

func normalizeProviderFormat(format string) string {
	return strings.ToLower(strings.TrimSpace(format))
}

func normalizeProviderBackend(backend string) string {
	return strings.ToLower(strings.TrimSpace(backend))
}

func normalizeRouteProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}
