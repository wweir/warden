package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

type Deps struct {
	Cfg                *config.ConfigStruct
	Selector           *sel.Selector
	ApplyMetricHeaders func(w http.ResponseWriter, r *http.Request, route *config.RouteConfig, serviceProtocol, endpoint, providerName string, target *sel.RouteTarget) telemetrypkg.Labels
	LogRequest         func(r *http.Request, provider, model string)
	RecordFailover     func(labels telemetrypkg.Labels)
	RecordTTFT         func(labels telemetrypkg.Labels, ttft time.Duration)
	RecordTokenMetrics func(labels telemetrypkg.Labels, usage telemetrypkg.TokenUsage, durationMs int64)
	RecordAndBroadcast func(record reqlog.Record)
}

type Handler struct {
	deps Deps
}

func NewHandler(deps Deps) *Handler {
	return &Handler{deps: deps}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	startTime := time.Now()
	reqID := reqlog.GenerateID()

	reqBody, err := readBody(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	model := gjson.GetBytes(reqBody, "model").String()
	stream := gjson.GetBytes(reqBody, "stream").Bool()
	explicitProvider := r.Header.Get("X-Provider")
	allowFailover := IsInferenceEndpoint(r.URL.Path)
	authRetried := map[string]bool{}
	serviceProtocol := ServiceProtocol(r.URL.Path, reqBody)
	endpoint := strings.TrimPrefix(r.URL.Path, "/")
	if serviceProtocol != "" && !route.SupportsServiceProtocol(serviceProtocol) {
		http.Error(w, UnsupportedRouteProtocolMessage(route.ConfiguredProtocol(), serviceProtocol), http.StatusBadRequest)
		return
	}
	if serviceProtocol == config.RouteProtocolResponsesStateful {
		allowFailover = false
	}

	var manager *inferencepkg.Manager
	var provCfg *config.ProviderConfig
	var target *sel.RouteTarget
	if model != "" {
		manager, err = inferencepkg.NewManager(
			h.deps.Cfg,
			h.deps.Selector,
			route,
			serviceProtocol,
			endpoint,
			model,
			explicitProvider,
			allowFailover,
			func(failed *inferencepkg.ResolvedTarget) {
				if h.deps.RecordFailover != nil {
					h.deps.RecordFailover(telemetrypkg.BuildMetricLabels(route, serviceProtocol, endpoint, failed.Target))
				}
			},
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		current := manager.Current()
		provCfg = current.Provider
		target = current.Target
	} else {
		provCfg = h.selectProviderWithoutModel(route, serviceProtocol, explicitProvider)
		if provCfg == nil {
			http.Error(w, "provider not found for route", http.StatusBadRequest)
			return
		}
	}

	var metricLabels telemetrypkg.Labels
	if h.deps.ApplyMetricHeaders != nil {
		metricLabels = h.deps.ApplyMetricHeaders(w, r, route, serviceProtocol, endpoint, provCfg.Name, target)
	}

	currentFailovers := func() []reqlog.Failover {
		if manager == nil {
			return nil
		}
		return manager.Failovers()
	}

	for {
		if h.deps.LogRequest != nil {
			h.deps.LogRequest(r, provCfg.Name, model)
		}

		provReqBody := reqBody
		if target != nil {
			provReqBody = upstreampkg.RewriteModelRaw(reqBody, target.UpstreamModel)
		}

		targetURL := provCfg.URL + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(provReqBody))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		proxyReq.Header = upstreampkg.BuildProxyRequestHeaders(r, allowFailover)
		sel.SetAuthHeaders(proxyReq.Header, provCfg)

		upstreamStart := time.Now()
		resp, err := provCfg.HTTPClient(0).Do(proxyReq)
		latency := time.Since(upstreamStart)
		if err != nil {
			if allowFailover {
				h.deps.Selector.RecordOutcome(provCfg.Name, err, latency)
			}
			if manager == nil && inferencepkg.TryAuthRetry(err, provCfg, authRetried) {
				continue
			}
			if manager != nil && manager.HandleError(err) {
				current := manager.Current()
				provCfg = current.Provider
				target = current.Target
				if h.deps.ApplyMetricHeaders != nil {
					metricLabels = h.deps.ApplyMetricHeaders(w, r, route, serviceProtocol, endpoint, provCfg.Name, target)
				}
				continue
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		contentEncoding := upstreampkg.NormalizeContentEncoding(resp.Header.Get("Content-Encoding"))
		decodedRespBody, decodeErr := upstreampkg.DecodeResponseBody(contentEncoding, respBody)
		inspectableBody := decodeErr == nil
		errBody := string(respBody)
		if inspectableBody {
			errBody = string(decodedRespBody)
		} else {
			errBody = upstreampkg.CompressedBodyPlaceholder(contentEncoding, len(respBody))
		}

		var upErr *sel.UpstreamError
		if resp.StatusCode != http.StatusOK {
			upErr = &sel.UpstreamError{Code: resp.StatusCode, Body: errBody}
		} else if inspectableBody {
			if errType, _ := sel.ParseErrorBody(string(decodedRespBody)); errType != "" && sel.IsRetryableByBody(string(decodedRespBody)) {
				upErr = &sel.UpstreamError{Code: resp.StatusCode, Body: string(decodedRespBody)}
			}
		}

		if upErr != nil {
			if allowFailover {
				h.deps.Selector.RecordOutcome(provCfg.Name, upErr, latency)
			}
			if manager == nil && inferencepkg.TryAuthRetry(upErr, provCfg, authRetried) {
				continue
			}
			if manager != nil && manager.HandleError(upErr) {
				current := manager.Current()
				provCfg = current.Provider
				target = current.Target
				if h.deps.ApplyMetricHeaders != nil {
					metricLabels = h.deps.ApplyMetricHeaders(w, r, route, serviceProtocol, endpoint, provCfg.Name, target)
				}
				continue
			}
		} else {
			h.deps.Selector.RecordOutcome(provCfg.Name, nil, latency)
			if stream && h.deps.RecordTTFT != nil {
				h.deps.RecordTTFT(metricLabels, latency)
			}
		}

		maps.Copy(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		if _, writeErr := w.Write(respBody); writeErr != nil {
			slog.Warn("Failed to write proxy response", "error", writeErr)
		}

		durationMs := time.Since(startTime).Milliseconds()
		logResp := []byte(errBody)
		if inspectableBody {
			logResp = AssembleResponse(serviceProtocol, provCfg.Protocol, decodedRespBody)
		}

		if resp.StatusCode == http.StatusOK && inspectableBody && len(logResp) > 0 && h.deps.RecordTokenMetrics != nil {
			h.deps.RecordTokenMetrics(metricLabels, telemetrypkg.ExtractTokenUsage(logResp), durationMs)
		}

		if h.deps.RecordAndBroadcast != nil {
			rec := reqlog.Record{
				Timestamp:   startTime,
				RequestID:   reqID,
				Route:       route.Prefix,
				Endpoint:    r.URL.Path,
				Model:       model,
				Provider:    provCfg.Name,
				UserAgent:   r.UserAgent(),
				DurationMs:  durationMs,
				Fingerprint: reqlog.BuildFingerprint(reqBody),
				Request:     reqBody,
				Response:    logResp,
				Failovers:   currentFailovers(),
			}
			if upErr != nil {
				rec.Error = upErr.Error()
			}
			h.deps.RecordAndBroadcast(rec)
		}
		return
	}
}

func (h *Handler) HandleModels(w http.ResponseWriter, route *config.RouteConfig) {
	models := h.deps.Selector.Models(route)
	if models == nil {
		models = []json.RawMessage{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
	})
}

func (h *Handler) selectProviderWithoutModel(route *config.RouteConfig, serviceProtocol, explicitProvider string) *config.ProviderConfig {
	for _, providerName := range route.ProviderNames() {
		if explicitProvider != "" && providerName != explicitProvider {
			continue
		}
		candidate := h.deps.Cfg.Provider[providerName]
		if candidate == nil {
			continue
		}
		if serviceProtocol != "" && !config.ProviderSupportsConfiguredProtocol(candidate, serviceProtocol) {
			continue
		}
		return candidate
	}
	return nil
}

func IsInferenceEndpoint(path string) bool {
	if strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") {
		return true
	}
	return path == "/messages" || strings.HasSuffix(path, "/messages")
}

func AssembleResponse(serviceProtocol, upstreamProtocol string, body []byte) []byte {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "event:") && !strings.HasPrefix(trimmed, "data:") {
		return body
	}
	if serviceProtocol == config.RouteProtocolAnthropic {
		if assembled := anthropic.AssembleStream(body); assembled != nil {
			return assembled
		}
		return MarshalRawStreamForLog(body)
	}
	if config.IsResponsesRouteProtocol(serviceProtocol) {
		if assembled, err := openai.AssembleResponsesStream(body); err == nil {
			return assembled
		}
		return MarshalRawStreamForLog(body)
	}
	clientBody := upstreampkg.ConvertStreamIfNeeded(upstreamProtocol, body)
	if assembled, err := openai.AssembleChatStream(clientBody); err == nil {
		return assembled
	}
	return MarshalRawStreamForLog(clientBody)
}

func ServiceProtocol(path string, reqBody []byte) string {
	if strings.HasSuffix(path, "/messages") || path == "/messages" {
		return config.RouteProtocolAnthropic
	}
	if strings.HasSuffix(path, "/chat/completions") {
		return config.RouteProtocolChat
	}
	if strings.HasSuffix(path, "/responses") {
		return ResponsesRequestProtocol(reqBody)
	}
	return ""
}

func UnsupportedRouteProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolResponsesStateful:
		return UnsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol)
	case config.RouteProtocolResponsesStateless:
		return UnsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol)
	case config.RouteProtocolChat:
		return "route protocol " + routeProtocol + " does not support chat requests"
	case config.RouteProtocolAnthropic:
		return "route protocol " + routeProtocol + " does not support anthropic messages requests"
	default:
		return "route does not support this request protocol"
	}
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func MarshalRawStreamForLog(body []byte) []byte {
	data, err := json.Marshal(strings.TrimSpace(string(body)))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return data
}

func ResponsesRequestProtocol(rawReqBody []byte) string {
	if gjson.GetBytes(rawReqBody, "previous_response_id").String() != "" {
		return config.RouteProtocolResponsesStateful
	}
	return config.RouteProtocolResponsesStateless
}

func UnsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolResponsesStateful:
		return fmt.Sprintf("route protocol %s does not support stateful responses requests", routeProtocol)
	case config.RouteProtocolResponsesStateless:
		return fmt.Sprintf("route protocol %s does not support stateless responses requests", routeProtocol)
	default:
		return fmt.Sprintf("route protocol %s does not support responses requests", routeProtocol)
	}
}
