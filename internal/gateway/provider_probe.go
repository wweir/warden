package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	sel "github.com/wweir/warden/internal/selector"
	anthproto "github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

const providerProbeTimeout = 15 * time.Second

func (g *Gateway) handleProviderProtocolDetect(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	provCfg := g.cfg.Provider[body.Name]
	if provCfg == nil {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	displayProtocols, probe := detectProviderDisplayProtocols(provCfg)
	if len(displayProtocols) == 0 {
		displayProtocols = config.SupportedRouteProtocols(provCfg)
	}
	g.selector.SetDisplayProtocols(body.Name, displayProtocols, &probe)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":                 body.Name,
		"display_protocols":    displayProtocols,
		"candidate_protocols":  config.CandidateRouteProtocols(provCfg),
		"configured_protocols": config.SupportedRouteProtocols(provCfg),
		"probe":                probe,
	})
}

func (g *Gateway) handleProviderModelProtocolProbe(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	var body struct {
		Name     string `json:"name"`
		Model    string `json:"model"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.Model == "" || body.Protocol == "" {
		http.Error(w, "name, model, protocol are required", http.StatusBadRequest)
		return
	}
	provCfg := g.cfg.Provider[body.Name]
	if provCfg == nil {
		http.Error(w, "unknown provider: "+body.Name, http.StatusNotFound)
		return
	}

	probe := probeProviderModelProtocol(provCfg, strings.TrimSpace(body.Model), strings.TrimSpace(body.Protocol))
	probe.Model = strings.TrimSpace(body.Model)
	probe.Protocol = strings.TrimSpace(body.Protocol)
	g.selector.UpsertModelProtocolProbe(body.Name, probe)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(probe)
}

func detectProviderDisplayProtocols(provCfg *config.ProviderConfig) ([]string, sel.ProtocolProbe) {
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
		reachable, err := lightProbeEndpoint(provCfg, endpoint)
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
		return protocolEndpoint(providerProtocol, false)
	case config.RouteProtocolResponsesStateless, config.RouteProtocolResponsesStateful:
		if providerProtocol != "openai" {
			return ""
		}
		return protocolEndpoint(providerProtocol, true)
	case config.RouteProtocolAnthropic:
		if providerProtocol != "anthropic" {
			return ""
		}
		return protocolEndpoint(providerProtocol, false)
	default:
		return ""
	}
}

func lightProbeEndpoint(provCfg *config.ProviderConfig, endpoint string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), providerProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodOptions, provCfg.URL+endpoint, nil)
	if err != nil {
		return false, err
	}
	sel.SetAuthHeaders(req.Header, provCfg)
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

func probeProviderModelProtocol(provCfg *config.ProviderConfig, model, protocol string) sel.ModelProtocolProbe {
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
		err := sendChatProbe(provCfg, model)
		applyProbeError(&probe, err)
	case config.RouteProtocolResponsesStateless:
		err := sendResponsesProbe(provCfg, model, "")
		applyProbeError(&probe, err)
	case config.RouteProtocolResponsesStateful:
		firstID, err := sendResponsesProbeAndExtractID(provCfg, model)
		if err != nil {
			applyProbeError(&probe, err)
			return probe
		}
		err = sendResponsesProbe(provCfg, model, firstID)
		applyProbeError(&probe, err)
	case config.RouteProtocolAnthropic:
		err := sendAnthropicProbe(provCfg, model)
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

func sendChatProbe(provCfg *config.ProviderConfig, model string) error {
	req := openai.ChatCompletionRequest{
		Model: model,
		Messages: []openai.Message{
			{Role: "user", Content: "ping"},
		},
		Extra: map[string]json.RawMessage{
			"max_tokens": json.RawMessage("1"),
		},
	}
	body, err := marshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerProbeTimeout)
	defer cancel()
	_, _, err = sendRequest(ctx, provCfg, protocolEndpoint(provCfg.Protocol, false), body, false)
	return err
}

func sendAnthropicProbe(provCfg *config.ProviderConfig, model string) error {
	if provCfg.Protocol == "openai" && provCfg.AnthropicToChat {
		rawBody := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model))
		chatReq, err := anthproto.MessagesRequestToChatRequest(rawBody)
		if err != nil {
			return err
		}
		body, err := marshalProtocolRequest(provCfg.Protocol, chatReq)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), providerProbeTimeout)
		defer cancel()
		_, _, err = sendRequest(ctx, provCfg, protocolEndpoint(provCfg.Protocol, false), body, false)
		return err
	}

	if provCfg.Protocol != "anthropic" {
		return fmt.Errorf("provider family does not support anthropic probe")
	}
	body := []byte(fmt.Sprintf(`{"model":%q,"max_tokens":1,"messages":[{"role":"user","content":"ping"}]}`, model))
	ctx, cancel := context.WithTimeout(context.Background(), providerProbeTimeout)
	defer cancel()
	_, _, err := sendRequest(ctx, provCfg, protocolEndpoint(provCfg.Protocol, false), body, false)
	return err
}

func sendResponsesProbe(provCfg *config.ProviderConfig, model, previousResponseID string) error {
	_, _, err := sendResponsesProbeRaw(provCfg, model, previousResponseID)
	return err
}

func sendResponsesProbeAndExtractID(provCfg *config.ProviderConfig, model string) (string, error) {
	body, _, err := sendResponsesProbeRaw(provCfg, model, "")
	if err != nil {
		return "", err
	}
	id := gjson.GetBytes(body, "id").String()
	if id == "" {
		return "", fmt.Errorf("probe response missing id")
	}
	return id, nil
}

func sendResponsesProbeRaw(provCfg *config.ProviderConfig, model, previousResponseID string) ([]byte, time.Duration, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), providerProbeTimeout)
	defer cancel()
	return sendRequest(ctx, provCfg, protocolEndpoint(provCfg.Protocol, true), body, false)
}
