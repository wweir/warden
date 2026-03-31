package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	sel "github.com/wweir/warden/internal/selector"
	anthproto "github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

const providerProbeTimeout = 15 * time.Second

func detectProviderDisplayProtocols(ctx context.Context, provCfg *config.ProviderConfig) ([]string, sel.ProtocolProbe) {
	candidates := config.SupportedRouteProtocols(provCfg)
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
		endpoint := protocolProbeEndpoint(provCfg.Protocol, protocol)
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
		endpoint := protocolProbeEndpoint(provCfg.Protocol, protocol)
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

func protocolProbeEndpoint(providerProtocol, configuredProtocol string) string {
	switch configuredProtocol {
	case config.RouteProtocolChat:
		return upstreampkg.ProtocolEndpoint(providerProtocol, false)
	case config.RouteProtocolResponsesStateless, config.RouteProtocolResponsesStateful:
		if providerProtocol != "openai" {
			return ""
		}
		return upstreampkg.ProtocolEndpoint(providerProtocol, true)
	case config.RouteProtocolAnthropic:
		if providerProtocol != "anthropic" {
			return ""
		}
		return upstreampkg.ProtocolEndpoint(providerProtocol, false)
	default:
		return ""
	}
}

func lightProbeEndpoint(ctx context.Context, provCfg *config.ProviderConfig, endpoint string) (bool, error) {
	ctx, cancel := probeContext(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, provCfg.URL+endpoint, nil)
	if err != nil {
		return false, err
	}
	sel.SetAuthHeaders(ctx, req.Header, provCfg)
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
	case config.RouteProtocolResponsesStateless:
		err := sendResponsesProbe(ctx, provCfg, model, "")
		applyProbeError(&probe, err)
	case config.RouteProtocolResponsesStateful:
		firstID, err := sendResponsesProbeAndExtractID(ctx, provCfg, model)
		if err != nil {
			applyProbeError(&probe, err)
			return probe
		}
		err = sendResponsesProbe(ctx, provCfg, model, firstID)
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
		probe.Error = upErr.Error()
		return
	}
	probe.Status = "error"
	probe.Error = err.Error()
}

func sendChatProbe(ctx context.Context, provCfg *config.ProviderConfig, model string) error {
	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.Message{
			{Role: "user", Content: "ping"},
		},
		Extra: map[string]json.RawMessage{
			"max_tokens": json.RawMessage("1"),
		},
	}
	body, err := upstreampkg.MarshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return err
	}
	return sendProbeRequest(ctx, provCfg, upstreampkg.ProtocolEndpoint(provCfg.Protocol, false), body)
}

func sendAnthropicProbe(ctx context.Context, provCfg *config.ProviderConfig, model string) error {
	if provCfg.Protocol == "openai" && provCfg.AnthropicToChat {
		rawBody := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model))
		chatReq, err := anthproto.MessagesRequestToChatRequest(rawBody)
		if err != nil {
			return err
		}
		body, err := upstreampkg.MarshalProtocolRequest(provCfg.Protocol, chatReq)
		if err != nil {
			return err
		}
		return sendProbeRequest(ctx, provCfg, upstreampkg.ProtocolEndpoint(provCfg.Protocol, false), body)
	}

	if provCfg.Protocol != "anthropic" {
		return fmt.Errorf("provider family does not support anthropic probe")
	}
	body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model))
	return sendProbeRequest(ctx, provCfg, upstreampkg.ProtocolEndpoint(provCfg.Protocol, false), body)
}

func sendResponsesProbe(ctx context.Context, provCfg *config.ProviderConfig, model, previousResponseID string) error {
	_, _, err := sendResponsesProbeRaw(ctx, provCfg, model, previousResponseID)
	return err
}

func sendResponsesProbeAndExtractID(ctx context.Context, provCfg *config.ProviderConfig, model string) (string, error) {
	body, _, err := sendResponsesProbeRaw(ctx, provCfg, model, "")
	if err != nil {
		return "", err
	}
	id := gjson.GetBytes(body, "id").String()
	if id == "" {
		return "", fmt.Errorf("probe response missing id")
	}
	return id, nil
}

func sendResponsesProbeRaw(ctx context.Context, provCfg *config.ProviderConfig, model, previousResponseID string) ([]byte, time.Duration, error) {
	if provCfg.Protocol != "openai" {
		return nil, 0, fmt.Errorf("provider family does not support responses probe")
	}
	payload := map[string]any{
		"model":             model,
		"input":             "ping",
		"max_output_tokens": 1,
		"store":             false,
	}
	if previousResponseID != "" {
		payload["previous_response_id"] = previousResponseID
	}
	body, _ := json.Marshal(payload)
	ctx, cancel := probeContext(ctx)
	defer cancel()
	return upstreampkg.SendRequest(ctx, nil, provCfg, upstreampkg.ProtocolEndpoint(provCfg.Protocol, true), body, false)
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
