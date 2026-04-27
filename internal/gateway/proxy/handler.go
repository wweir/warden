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
