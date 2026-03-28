package gateway

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/internal/reqlog"
	sel "github.com/wweir/warden/internal/selector"
)

type inferenceRequest struct {
	RawBody          []byte
	Model            string
	Stream           bool
	ExplicitProvider string
}

type inferenceBootstrap struct {
	request   *http.Request
	req       inferenceRequest
	startTime time.Time
	requestID string
}

func withRouteRequestContext(r *http.Request, route *config.RouteConfig) *http.Request {
	return r.WithContext(requestctxpkg.WithRouteHooks(requestctxpkg.WithClientRequest(r.Context(), r), route.Hooks))
}

func bootstrapInferenceRequest(r *http.Request, route *config.RouteConfig) (inferenceBootstrap, error) {
	r = withRouteRequestContext(r, route)
	req, err := readJSONInferenceRequest(r)
	if err != nil {
		return inferenceBootstrap{}, err
	}
	return inferenceBootstrap{
		request:   r,
		req:       req,
		startTime: time.Now(),
		requestID: reqlog.GenerateID(),
	}, nil
}

func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func readJSONInferenceRequest(r *http.Request) (inferenceRequest, error) {
	rawBody, err := readRequestBody(r)
	if err != nil {
		return inferenceRequest{}, err
	}
	if !gjson.ValidBytes(rawBody) {
		return inferenceRequest{}, fmt.Errorf("invalid JSON")
	}
	return inferenceRequest{
		RawBody:          rawBody,
		Model:            gjson.GetBytes(rawBody, "model").String(),
		Stream:           gjson.GetBytes(rawBody, "stream").Bool(),
		ExplicitProvider: r.Header.Get("X-Provider"),
	}, nil
}

func (g *Gateway) buildInferenceManager(
	w http.ResponseWriter,
	route *config.RouteConfig,
	serviceProtocol, endpoint string,
	req inferenceRequest,
	allowFailover bool,
) (*inferencepkg.Manager, bool) {
	manager, err := g.newInferenceManager(route, serviceProtocol, endpoint, req, allowFailover)
	if err != nil {
		writeModelSelectionError(w, err)
		return nil, false
	}
	return manager, true
}

func applyRouteModelPrompt(req *inferenceRequest, routeModel *config.CompiledRouteModel, inject func([]byte, string) []byte) {
	if routeModel == nil || !routeModel.PromptEnabled {
		return
	}
	if prompt := routeModel.SystemPrompt; prompt != "" {
		req.RawBody = inject(req.RawBody, prompt)
	}
}

func prepareRawBody(rawBody []byte, target *sel.RouteTarget) []byte {
	if target == nil {
		return rawBody
	}
	return upstreampkg.RewriteModelRaw(rawBody, target.UpstreamModel)
}

func applyInferenceMetricHeaders(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	serviceProtocol, endpoint, providerName string,
	target *sel.RouteTarget,
) telemetrypkg.Labels {
	labels := telemetrypkg.BuildMetricLabels(route, serviceProtocol, endpoint, target)
	labels.APIKey = requestctxpkg.APIKeyNameFromContext(r.Context())
	if labels.Provider == "" {
		labels.Provider = providerName
	}
	telemetrypkg.ApplyMetricHeaders(w, labels)
	return labels
}

func writeEventStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeJSONResponse(w http.ResponseWriter, body []byte, warnMsg string) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(body); err != nil {
		slog.Warn(warnMsg, "error", err)
	}
}

func writeStreamResponse(w http.ResponseWriter, body []byte, warnMsg string) {
	if _, err := w.Write(body); err != nil {
		slog.Warn(warnMsg, "error", err)
	}
	w.(http.Flusher).Flush()
}
