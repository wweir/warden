package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/wweir/warden/config"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/internal/providerauth"
	sel "github.com/wweir/warden/internal/selector"
	anthproto "github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

const providerProbeTimeout = 15 * time.Second

func detectProviderDisplayProtocols(ctx context.Context, provCfg *config.ProviderConfig) ([]string, sel.ProtocolProbe) {
	candidates := config.SupportedDisplayProtocols(provCfg)
	now := time.Now()
	if len(candidates) == 0 {
		return nil, sel.ProtocolProbe{
			CheckedAt: now,
			Status:    "error",
			Source:    "light_probe",
			Error:     "no candidate protocols",
		}
	}

	type endpointState struct {
		reachable bool
		err       error
	}
	endpointResults := map[string]endpointState{}
	var firstErr error
	for _, protocol := range candidates {
		endpoint := protocolProbeEndpoint(provCfg, protocol)
		if endpoint == "" {
			continue
		}
		if _, exists := endpointResults[endpoint]; exists {
			continue
		}
		reachable, err := lightProbeEndpoint(ctx, provCfg, endpoint)
		endpointResults[endpoint] = endpointState{reachable: reachable, err: err}
		if firstErr == nil && err != nil {
			firstErr = err
		}
	}

	var display []string
	for _, protocol := range candidates {
		endpoint := protocolProbeEndpoint(provCfg, protocol)
		if endpoint == "" {
			continue
		}
		if endpointResults[endpoint].reachable {
			display = append(display, protocol)
		}
	}
	if len(display) == 0 {
		display = append([]string(nil), candidates...)
	}

	probe := sel.ProtocolProbe{
		CheckedAt: now,
		Status:    "ok",
		Source:    "light_probe",
	}
	if firstErr != nil {
		probe.Status = "error"
		probe.Error = firstErr.Error()
	}
	return display, probe
}

func protocolProbeEndpoint(provCfg *config.ProviderConfig, configuredProtocol string) string {
	// Determine the effective access mode for this provider
	modes := config.ProviderFormats(provCfg)
	if len(modes) == 0 {
		// Fallback to legacy protocol
		return protocolProbeEndpointLegacy(provCfg, provCfg.Format, configuredProtocol)
	}

	// For probing purposes, we need to know which access modes support this protocol
	switch configuredProtocol {
	case config.RouteProtocolChat:
		format := modes[0]
		return formatProbeURL(provCfg, format, upstreampkg.ProtocolEndpointForFormat(format, config.RouteProtocolChat, false))
	case config.RouteProtocolResponses:
		for _, mode := range modes {
			if mode == config.ProviderFormatOpenAI {
				return formatProbeURL(provCfg, mode, upstreampkg.ProtocolEndpointForFormat(mode, config.RouteProtocolResponses, false))
			}
			if mode == config.ProviderFormatAnthropic && config.FormatHasBridge(provCfg, mode, "anthropic_to_responses") {
				return formatProbeURL(provCfg, mode, upstreampkg.ProtocolEndpointForFormat(mode, config.RouteProtocolAnthropic, false))
			}
		}
		return ""
	case config.RouteProtocolAnthropic:
		for _, mode := range modes {
			if mode == config.ProviderFormatAnthropic {
				return formatProbeURL(provCfg, mode, upstreampkg.ProtocolEndpointForFormat(mode, config.RouteProtocolAnthropic, false))
			}
			if mode == config.ProviderFormatOpenAI && config.FormatHasBridge(provCfg, mode, "anthropic_to_chat") {
				return formatProbeURL(provCfg, mode, upstreampkg.ProtocolEndpointForFormat(mode, config.RouteProtocolAnthropic, true))
			}
		}
		return ""
	case config.ServiceProtocolEmbeddings:
		for _, mode := range modes {
			if mode == config.ProviderFormatOpenAI {
				return formatProbeURL(provCfg, mode, upstreampkg.EmbeddingsEndpoint())
			}
		}
		return ""
	default:
		return ""
	}
}

func protocolProbeEndpointLegacy(provCfg *config.ProviderConfig, providerProtocol, configuredProtocol string) string {
	switch configuredProtocol {
	case config.RouteProtocolChat:
		return formatProbeURL(provCfg, providerProtocol, upstreampkg.ProtocolEndpoint(providerProtocol, false))
	case config.RouteProtocolResponses:
		if providerProtocol != config.ProviderFormatOpenAI {
			return ""
		}
		return formatProbeURL(provCfg, providerProtocol, upstreampkg.ProtocolEndpoint(providerProtocol, true))
	case config.RouteProtocolAnthropic:
		if providerProtocol != config.ProviderFormatAnthropic {
			return ""
		}
		return formatProbeURL(provCfg, providerProtocol, upstreampkg.ProtocolEndpoint(providerProtocol, false))
	case config.ServiceProtocolEmbeddings:
		if providerProtocol != config.ProviderFormatOpenAI {
			return ""
		}
		return formatProbeURL(provCfg, providerProtocol, upstreampkg.EmbeddingsEndpoint())
	default:
		return ""
	}
}

func formatProbeURL(provCfg *config.ProviderConfig, format, endpoint string) string {
	if provCfg == nil {
		return ""
	}
	baseURL := config.FormatEffectiveURL(provCfg, format)
	if baseURL == "" {
		baseURL = provCfg.URL
	}
	return upstreampkg.JoinBaseURLPath(baseURL, endpoint)
}

func lightProbeEndpoint(ctx context.Context, provCfg *config.ProviderConfig, targetURL string) (bool, error) {
	ctx, cancel := probeContext(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, targetURL, nil)
	if err != nil {
		return false, err
	}
	if err := providerauth.SetHeaders(ctx, req.Header, provCfg, targetURL); err != nil {
		return false, err
	}
	resp, err := provCfg.HTTPClient(providerProbeTimeout).Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return true, nil
}

func probeProviderModelProtocol(ctx context.Context, provCfg *config.ProviderConfig, model, protocol string) sel.ModelProtocolProbe {
	probe := sel.ModelProtocolProbe{
		Model:     model,
		Protocol:  protocol,
		CheckedAt: time.Now(),
		Status:    "unsupported",
	}

	if !config.ProviderSupportsConfiguredProtocol(provCfg, protocol) {
		probe.Error = "provider family does not support this protocol"
		return probe
	}

	switch protocol {
	case config.RouteProtocolChat:
		err := sendChatProbe(ctx, provCfg, model)
		applyProbeError(&probe, err)
	case config.RouteProtocolResponses:
		err := sendResponsesProbe(ctx, provCfg, model)
		applyProbeError(&probe, err)
	case config.RouteProtocolAnthropic:
		err := sendAnthropicProbe(ctx, provCfg, model)
		applyProbeError(&probe, err)
	default:
		probe.Error = "unsupported probe protocol"
	}

	return probe
}

func applyProbeError(probe *sel.ModelProtocolProbe, err error) {
	if err == nil {
		probe.Status = "supported"
		probe.Error = ""
		return
	}
	var upErr *sel.UpstreamError
	if errors.As(err, &upErr) {
		switch upErr.Code {
		case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
			probe.Status = "unsupported"
		default:
			probe.Status = "error"
		}
		probe.Error = sanitizeProbeError(upErr)
		return
	}
	probe.Status = "error"
	probe.Error = sanitizeProbeError(err)
}

func inferProbeFormat(provCfg *config.ProviderConfig, preferAnthropic bool) string {
	modes := config.ProviderFormats(provCfg)
	if len(modes) > 0 {
		if preferAnthropic {
			for _, m := range modes {
				if m == config.ProviderFormatAnthropic {
					return m
				}
			}
		}
		return modes[0]
	}
	if provCfg.Format == config.ProviderFormatAnthropic {
		return config.ProviderFormatAnthropic
	}
	return config.ProviderFormatOpenAI
}

func sendChatProbe(ctx context.Context, provCfg *config.ProviderConfig, model string) error {
	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.Message{
			{Role: "user", Content: "ping"},
		},
	}
	accessMode := inferProbeFormat(provCfg, false)
	body, err := upstreampkg.MarshalProtocolRequest(string(accessMode), req)
	if err != nil {
		return err
	}
	return sendProbeRequest(ctx, provCfg, formatProbeURL(provCfg, accessMode, upstreampkg.ProtocolEndpointForFormat(accessMode, config.RouteProtocolChat, false)), body)
}

func sendAnthropicProbe(ctx context.Context, provCfg *config.ProviderConfig, model string) error {
	// Check openai access mode with anthropic_to_chat bridge (new access mode or legacy field)
	if config.FormatHasBridge(provCfg, config.ProviderFormatOpenAI, "anthropic_to_chat") || provCfg.AnthropicToChat {
		rawBody := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model))
		chatReq, err := anthproto.MessagesRequestToChatRequest(rawBody)
		if err != nil {
			return err
		}
		body, err := upstreampkg.MarshalProtocolRequest(string(config.ProviderFormatOpenAI), chatReq)
		if err != nil {
			return err
		}
		return sendProbeRequest(ctx, provCfg, formatProbeURL(provCfg, config.ProviderFormatOpenAI, upstreampkg.ProtocolEndpointForFormat(config.ProviderFormatOpenAI, config.RouteProtocolChat, false)), body)
	}

	// Check native anthropic access mode
	modes := config.ProviderFormats(provCfg)
	hasAnthropic := provCfg.Format == config.ProviderFormatAnthropic
	for _, m := range modes {
		if m == config.ProviderFormatAnthropic {
			hasAnthropic = true
			break
		}
	}
	if !hasAnthropic {
		return fmt.Errorf("provider does not support anthropic protocol")
	}
	body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model))
	return sendProbeRequest(ctx, provCfg, formatProbeURL(provCfg, config.ProviderFormatAnthropic, upstreampkg.ProtocolEndpointForFormat(config.ProviderFormatAnthropic, config.RouteProtocolAnthropic, false)), body)
}

func sendResponsesProbe(ctx context.Context, provCfg *config.ProviderConfig, model string) error {
	modes := config.ProviderFormats(provCfg)
	hasOpenAI := provCfg.Format == config.ProviderFormatOpenAI
	hasAnthropicBridge := false
	for _, m := range modes {
		if m == config.ProviderFormatOpenAI {
			hasOpenAI = true
		}
		if m == config.ProviderFormatAnthropic && config.FormatHasBridge(provCfg, m, "anthropic_to_responses") {
			hasAnthropicBridge = true
		}
	}
	if !hasOpenAI && !hasAnthropicBridge {
		return fmt.Errorf("provider does not support responses protocol")
	}
	if hasOpenAI {
		payload := map[string]any{
			"model":            model,
			"input":            "ping",
			"max_output_tokens": 1,
			"store":            false,
		}
		body, _ := json.Marshal(payload)
		ctx, cancel := probeContext(ctx)
		defer cancel()
		return sendProbeRequest(ctx, provCfg, formatProbeURL(provCfg, config.ProviderFormatOpenAI, upstreampkg.ProtocolEndpointForFormat(config.ProviderFormatOpenAI, config.RouteProtocolResponses, false)), body)
	}
	if !hasAnthropicBridge {
		return sendAnthropicProbe(ctx, provCfg, model)
	}
	chatReq, err := openai.ResponsesRequestToChatRequest(openai.ResponsesRequest{
		Model: model,
		Input: json.RawMessage(`"ping"`),
		Extra: map[string]json.RawMessage{
			"max_output_tokens": json.RawMessage(`1`),
			"store":             json.RawMessage(`false`),
		},
	})
	if err != nil {
		return err
	}
	body, err := upstreampkg.MarshalProtocolRequest(string(config.ProviderFormatAnthropic), chatReq)
	if err != nil {
		return err
	}
	return sendProbeRequest(ctx, provCfg, formatProbeURL(provCfg, config.ProviderFormatAnthropic, upstreampkg.ProtocolEndpointForFormat(config.ProviderFormatAnthropic, config.RouteProtocolAnthropic, false)), body)
}

func sendProbeRequest(ctx context.Context, provCfg *config.ProviderConfig, endpoint string, body []byte) error {
	ctx, cancel := probeContext(ctx)
	defer cancel()

	_, _, err := upstreampkg.SendRequest(ctx, nil, provCfg, endpoint, body, false)
	return err
}

func probeContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, providerProbeTimeout)
}

type AccessModeProbeResult struct {
	Mode         string   `json:"mode"`
	Available    bool     `json:"available"`
	ResolvedURL  string   `json:"resolved_url,omitempty"`
	ModelsCount  int      `json:"models_count"`
	Error        string   `json:"error,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func (h *Handler) HandleProviderProbeAccess(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name    string            `json:"name"`
		URL     string            `json:"url"`
		APIKey  string            `json:"api_key"`
		Headers map[string]string `json:"headers"`
		Proxy   string            `json:"proxy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}
	apiKey := h.resolveProbeAPIKey(body.Name, body.APIKey)

	results := []AccessModeProbeResult{
		probeOpenAIAccess(r.Context(), body.URL, apiKey, body.Headers, body.Proxy),
		probeAnthropicAccess(r.Context(), body.URL, apiKey, body.Headers, body.Proxy),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"capabilities": mergeProbeCapabilities(results),
		"formats":      results,
	})
}

func (h *Handler) resolveProbeAPIKey(name, apiKey string) string {
	if apiKey != RedactedPlaceholder && apiKey != "***" {
		return apiKey
	}
	if h == nil || h.cfg == nil || name == "" {
		return apiKey
	}
	provCfg := h.cfg.Provider[name]
	if provCfg == nil {
		return apiKey
	}
	if stored := provCfg.APIKey.Value(); stored != "" {
		return stored
	}
	return apiKey
}

var (
	probeSecretPatternAPIKey  = regexp.MustCompile(`(?i)\bapi[_-]?key\b[:=]\s*\S+`)
	probeSecretPatternAuth    = regexp.MustCompile(`(?i)\bauthorization\b[:=]\s*\S+`)
	probeSecretPatternCookie  = regexp.MustCompile(`(?i)\bcookie\b[:=]\s*\S+`)
	probeSecretPatternXAPIKey = regexp.MustCompile(`(?i)\bx-api-key\b[:=]\s*\S+`)
	probeSecretPatternToken   = regexp.MustCompile(`\bsk-[a-zA-Z0-9]{10,}\b`)
)

func mergeProbeCapabilities(results []AccessModeProbeResult) []string {
	seen := map[string]bool{}
	var caps []string
	for _, r := range results {
		if !r.Available {
			continue
		}
		for _, c := range r.Capabilities {
			if !seen[c] {
				seen[c] = true
				caps = append(caps, c)
			}
		}
	}
	return caps
}

func sanitizeProbeError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	s = probeSecretPatternAPIKey.ReplaceAllString(s, "api_key=***")
	s = probeSecretPatternAuth.ReplaceAllString(s, "Authorization=***")
	s = probeSecretPatternCookie.ReplaceAllString(s, "Cookie=***")
	s = probeSecretPatternXAPIKey.ReplaceAllString(s, "x-api-key=***")
	s = probeSecretPatternToken.ReplaceAllString(s, "***")
	return s
}

// probeURLVariants returns baseURL and its /v1 counterpart (or vice versa).
func probeURLVariants(baseURL string) []string {
	urls := []string{baseURL}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return urls
	}
	path := parsed.Path
	if strings.HasSuffix(path, "/v1") {
		alt := *parsed
		alt.Path = strings.TrimSuffix(path, "/v1")
		if alt.Path == "" {
			alt.Path = "/"
		}
		urls = append(urls, alt.String())
	} else {
		alt := *parsed
		if path == "" || path == "/" {
			alt.Path = "/v1"
		} else {
			alt.Path = strings.TrimSuffix(path, "/") + "/v1"
		}
		urls = append(urls, alt.String())
	}
	return urls
}

func probeOpenAIAccess(ctx context.Context, baseURL, apiKey string, headers map[string]string, proxy string) AccessModeProbeResult {
	result := AccessModeProbeResult{
		Mode:         "openai",
		Available:    false,
		Capabilities: []string{},
	}

	for _, u := range probeURLVariants(baseURL) {
		provCfg := &config.ProviderConfig{
			URL:     u,
			APIKey:  config.SecretString(apiKey),
			Headers: headers,
			Proxy:   proxy,
			Format:  config.ProviderFormatOpenAI,
		}

		models, err := probeModels(ctx, provCfg, strings.TrimSuffix(u, "/")+"/models")
		if err == nil && len(models) > 0 {
			result.Available = true
			result.ResolvedURL = u
			result.ModelsCount = len(models)
			result.Error = ""
			result.Capabilities = []string{"chat", "responses", "embeddings"}
			return result
		}
		if result.Error == "" && err != nil {
			result.Error = sanitizeProbeError(err)
		}
	}

	return result
}

func probeAnthropicAccess(ctx context.Context, baseURL, apiKey string, headers map[string]string, proxy string) AccessModeProbeResult {
	result := AccessModeProbeResult{
		Mode:         "anthropic",
		Available:    false,
		Capabilities: []string{},
	}

	// Minimal Anthropic request with a non-existent model to avoid token consumption.
	probeBody := `{"model":"__warden_probe__","max_tokens":1,"messages":[]}`

	for _, u := range probeURLVariants(baseURL) {
		provCfg := &config.ProviderConfig{
			URL:     u,
			APIKey:  config.SecretString(apiKey),
			Headers: headers,
			Proxy:   proxy,
			Format:  config.ProviderFormatAnthropic,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(u, "/")+"/messages", strings.NewReader(probeBody))
		if err != nil {
			continue
		}
			if err := providerauth.SetHeaders(ctx, req.Header, provCfg, u); err != nil {
			continue
		}

		resp, err := provCfg.HTTPClient(providerProbeTimeout).Do(req)
		if err != nil {
			if result.Error == "" {
				result.Error = sanitizeProbeError(err)
			}
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		// Any response other than 404/502/503 means the endpoint exists.
		// 401/403 indicate auth issues, not a missing endpoint.
		if resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusServiceUnavailable {
			result.Available = true
			result.ResolvedURL = u
			result.Error = ""
			result.Capabilities = []string{"chat", "anthropic"}
			return result
		}
		if result.Error == "" {
			result.Error = sanitizeProbeError(fmt.Errorf("status %d", resp.StatusCode))
		}
	}

	return result
}

func probeModels(ctx context.Context, provCfg *config.ProviderConfig, url string) ([]string, error) {
	ctx, cancel := probeContext(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if err := providerauth.SetHeaders(ctx, req.Header, provCfg, url); err != nil {
		return nil, err
	}

	resp, err := provCfg.HTTPClient(providerProbeTimeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	models := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			models = append(models, m.ID)
		}
	}
	return models, nil
}
