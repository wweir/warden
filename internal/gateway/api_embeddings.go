package gateway

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/openai"
	"github.com/wweir/warden/pkg/toolhook"
)

// handleEmbeddings handles OpenAI-compatible embeddings requests.
func (g *Gateway) handleEmbeddings(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle embeddings", "route", route.Prefix) }()

	var bootstrap inferenceBootstrap
	bootstrap, err = bootstrapInferenceRequest(r, route)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = bootstrap.request
	req := bootstrap.req
	if req.Stream {
		http.Error(w, "stream is not supported for embeddings requests", http.StatusBadRequest)
		return
	}

	var embeddingsReq openai.EmbeddingsRequest
	if err := json.Unmarshal(req.RawBody, &embeddingsReq); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := embeddingsReq.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	manager, ok := g.buildInferenceManager(w, route, config.ServiceProtocolEmbeddings, "embeddings", req, req.ExplicitProvider == "")
	if !ok {
		return
	}

	g.handleBufferedInference(w, r, route, req, manager, bootstrap.startTime, bootstrap.requestID, bufferedInferenceSpec{
		serviceProtocol: config.ServiceProtocolEmbeddings,
		endpoint:        "embeddings",
		upstreamPath: func(_ string) string {
			return upstreampkg.EmbeddingsEndpoint()
		},
		prepareBody: func(_ string, rawBody []byte) ([]byte, error) {
			return rawBody, nil
		},
		runToolHooks: func(_ context.Context, _ string, respBody []byte, _ bool) ([]byte, []toolhook.HookVerdict, asyncHookFn) {
			return respBody, nil, func(func([]toolhook.HookVerdict)) {}
		},
		writeNonStream: func(w http.ResponseWriter, respBody []byte) {
			writeJSONResponse(w, respBody, "Failed to write embeddings response")
		},
		writeStream: func(w http.ResponseWriter, _ string, respBody []byte) {
			writeJSONResponse(w, respBody, "Failed to write embeddings response")
		},
		streamAssembler: func(string) observepkg.StreamLogAssembler {
			return nil
		},
	})
}
