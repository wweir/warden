package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/internal/providerauth"
	"github.com/wweir/warden/internal/reqlog"
	fingerprintpkg "github.com/wweir/warden/internal/reqlog/fingerprint"
	sel "github.com/wweir/warden/internal/selector"
)

type Deps struct {
	Cfg                *config.ConfigStruct
	Selector           *sel.Selector
	ApplyMetricHeaders func(w http.ResponseWriter, r *http.Request, route *config.RouteConfig, serviceProtocol, endpoint, providerName string, target *sel.RouteTarget) telemetrypkg.Labels
	LogRequest         func(r *http.Request, provider, model string)
	RecordFailover     func(labels telemetrypkg.Labels)
	RecordTTFT         func(labels telemetrypkg.Labels, ttft time.Duration)
	RecordTokenMetrics func(labels telemetrypkg.Labels, usage tokenusagepkg.Observation, durationMs int64)
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
	allowFailover := inferencepkg.IsInferenceEndpoint(r.URL.Path)
	authRetried := map[string]bool{}
	serviceProtocol := inferencepkg.ServiceProtocolFromRequest(r.URL.Path, reqBody)
	endpoint := strings.TrimPrefix(r.URL.Path, "/")
	if serviceProtocol != "" && !route.SupportsServiceProtocol(serviceProtocol) {
		http.Error(w, inferencepkg.UnsupportedRouteProtocolMessage(route.ConfiguredProtocol(), serviceProtocol), http.StatusBadRequest)
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
		providerauth.SetHeaders(r.Context(), proxyReq.Header, provCfg)

		upstreamStart := time.Now()
		resp, err := provCfg.HTTPClient(0).Do(proxyReq)
		latency := time.Since(upstreamStart)
		if err != nil {
			if allowFailover {
				h.deps.Selector.RecordOutcome(provCfg.Name, err, latency)
			}
			if r.Context().Err() != nil {
				return
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
			if r.Context().Err() != nil {
				return
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
			h.deps.Selector.ObserveMatchedModel(target)
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
			logResp = observepkg.AssembleResponse(serviceProtocol, provCfg.Protocol, decodedRespBody)
		}

		observation := tokenusagepkg.Missing(tokenusagepkg.SourceReportedJSON)
		if resp.StatusCode == http.StatusOK && inspectableBody {
			if stream {
				observation = tokenusagepkg.FromStream(serviceProtocol, provCfg.Protocol, decodedRespBody)
			} else {
				if serviceProtocol == config.ServiceProtocolEmbeddings {
					observation = tokenusagepkg.FromEmbeddingsJSON(logResp)
				} else {
					observation = tokenusagepkg.FromJSON(logResp)
				}
			}
			if h.deps.RecordTokenMetrics != nil {
				h.deps.RecordTokenMetrics(metricLabels, observation, durationMs)
			}
		}

		if h.deps.RecordAndBroadcast != nil {
			rec := reqlog.Record{
				Timestamp:   startTime,
				RequestID:   reqID,
				Route:       route.Prefix,
				Endpoint:    r.URL.Path,
				Model:       model,
				APIKey:      requestctxpkg.APIKeyNameFromContext(r.Context()),
				Provider:    provCfg.Name,
				UserAgent:   r.UserAgent(),
				DurationMs:  durationMs,
				Fingerprint: fingerprintpkg.BuildFingerprint(reqBody),
				Request:     reqBody,
				Response:    logResp,
				Failovers:   currentFailovers(),
			}
			if resp.StatusCode == http.StatusOK && inspectableBody {
				rec.TokenUsage = &reqlog.TokenUsage{
					PromptTokens:     observation.PromptTokens,
					CompletionTokens: observation.CompletionTokens,
					CacheTokens:      observation.CacheTokens,
					TotalTokens:      observation.TotalTokens,
					Source:           observation.SourceLabel(),
					Completeness:     observation.CompletenessLabel(),
				}
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
	codexModels := codexCompatibleModels(models)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
		"models": codexModels,
	})
}

func codexCompatibleModels(models []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(models))
	for _, model := range models {
		entry, ok := modelObject(model)
		if !ok {
			out = append(out, model)
			continue
		}
		id := rawString(entry["id"])
		if id == "" {
			id = rawString(entry["slug"])
		}
		if id == "" {
			out = append(out, model)
			continue
		}

		setStringDefault(entry, "slug", id)
		setStringDefault(entry, "display_name", id)
		setStringDefault(entry, "description", "")
		setStringDefault(entry, "default_reasoning_level", "medium")
		setRawDefault(entry, "supported_reasoning_levels", codexReasoningLevels)
		setStringDefault(entry, "shell_type", "shell_command")
		setStringDefault(entry, "visibility", "list")
		setRawDefault(entry, "supported_in_api", json.RawMessage("true"))
		setRawDefault(entry, "priority", json.RawMessage("0"))
		setRawDefault(entry, "additional_speed_tiers", json.RawMessage("[]"))
		setRawDefault(entry, "availability_nux", json.RawMessage("null"))
		setRawDefault(entry, "upgrade", json.RawMessage("null"))
		setStringDefault(entry, "base_instructions", "")
		setRawDefault(entry, "model_messages", json.RawMessage("{}"))
		setRawDefault(entry, "supports_reasoning_summaries", json.RawMessage("true"))
		setStringDefault(entry, "default_reasoning_summary", "none")
		setRawDefault(entry, "support_verbosity", json.RawMessage("true"))
		setStringDefault(entry, "default_verbosity", "medium")
		setStringDefault(entry, "apply_patch_tool_type", "freeform")
		setStringDefault(entry, "web_search_tool_type", "text")
		setRawDefault(entry, "truncation_policy", json.RawMessage(`{"mode":"tokens","limit":10000}`))
		setRawDefault(entry, "supports_parallel_tool_calls", json.RawMessage("true"))
		setRawDefault(entry, "supports_image_detail_original", json.RawMessage("false"))
		setRawDefault(entry, "context_window", json.RawMessage("128000"))
		setRawDefault(entry, "max_context_window", json.RawMessage("128000"))
		setRawDefault(entry, "effective_context_window_percent", json.RawMessage("95"))
		setRawDefault(entry, "experimental_supported_tools", json.RawMessage("[]"))
		setRawDefault(entry, "input_modalities", json.RawMessage(`["text"]`))
		setRawDefault(entry, "supports_search_tool", json.RawMessage("true"))

		encoded, err := json.Marshal(entry)
		if err != nil {
			out = append(out, model)
			continue
		}
		out = append(out, encoded)
	}
	return out
}

var codexReasoningLevels = json.RawMessage(`[
	{"effort":"low","description":"Fast responses with lighter reasoning"},
	{"effort":"medium","description":"Balances speed and reasoning depth for everyday tasks"},
	{"effort":"high","description":"Greater reasoning depth for complex problems"},
	{"effort":"xhigh","description":"Extra high reasoning depth for complex problems"}
]`)

func modelObject(model json.RawMessage) (map[string]json.RawMessage, bool) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(model, &entry); err != nil {
		return nil, false
	}
	return entry, true
}

func rawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func setStringDefault(entry map[string]json.RawMessage, key, value string) {
	if len(entry[key]) > 0 {
		return
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	entry[key] = encoded
}

func setRawDefault(entry map[string]json.RawMessage, key string, value json.RawMessage) {
	if len(entry[key]) > 0 {
		return
	}
	entry[key] = value
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
		if serviceProtocol != "" && !config.ProviderSupportsServiceProtocol(candidate, serviceProtocol) {
			continue
		}
		return candidate
	}
	return nil
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
