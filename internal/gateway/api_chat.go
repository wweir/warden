package gateway

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// handleChatCompletion handles Chat Completion requests.
func (g *Gateway) handleChatCompletion(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle chat completion", "route", route.Prefix) }()

	var bootstrap inferenceBootstrap
	bootstrap, err = bootstrapInferenceRequest(r, route)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = bootstrap.request
	req := bootstrap.req

	manager, ok := g.buildInferenceManager(w, route, config.RouteProtocolChat, "chat/completions", req, req.ExplicitProvider == "")
	if !ok {
		return
	}
	applyRouteModelPrompt(&req, manager.Current().Model, openai.InjectSystemPromptRaw)

	g.handleBufferedInference(w, r, route, req, manager, bootstrap.startTime, bootstrap.requestID, bufferedInferenceSpec{
		serviceProtocol: config.RouteProtocolChat,
		endpoint:        "chat/completions",
		upstreamPath: func(providerProtocol string) string {
			return upstreampkg.ProtocolEndpoint(providerProtocol, false)
		},
		prepareBody: func(providerProtocol string, rawBody []byte) ([]byte, error) {
			return upstreampkg.MarshalProtocolRaw(providerProtocol, rawBody)
		},
		runToolHooks: func(ctx context.Context, providerProtocol string, respBody []byte, stream bool) {
			observepkg.RunRouteToolHooks(ctx, g.cfg.Addr, observepkg.ParseChatToolCalls(providerProtocol, respBody, stream), "Chat: failed to run tool hooks")
		},
		writeStream: func(w http.ResponseWriter, providerProtocol string, respBody []byte) {
			writeEventStreamHeaders(w)
			clientBody := upstreampkg.ConvertStreamIfNeeded(providerProtocol, respBody)
			writeStreamResponse(w, clientBody, "Failed to write stream response")
		},
		writeNonStream: func(w http.ResponseWriter, respBody []byte) {
			writeJSONResponse(w, respBody, "Failed to write response")
		},
		streamAssembler: func(providerProtocol string) observepkg.StreamLogAssembler {
			return func(respBody []byte) ([]byte, []byte, error) {
				clientBody := upstreampkg.ConvertStreamIfNeeded(providerProtocol, respBody)
				assembled, err := openai.AssembleChatStream(clientBody)
				return assembled, clientBody, err
			}
		},
	})
}

// --- upstream communication ---

// forwardNonStreamRequest sends a non-streaming chat completion request upstream.
// Returns parsed response, raw body bytes, and first-token latency for passthrough optimization.
func (g *Gateway) forwardNonStreamRequest(ctx context.Context, provCfg *config.ProviderConfig, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, []byte, time.Duration, error) {
	var resp openai.ChatCompletionResponse

	reqBody, err := upstreampkg.MarshalProtocolRequest(provCfg.Protocol, req)
	if err != nil {
		return resp, nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	clientReq, _ := requestctxpkg.ClientRequestFromContext(ctx)
	body, latency, err := upstreampkg.SendRequest(ctx, clientReq, provCfg, upstreampkg.ProtocolEndpoint(provCfg.Protocol, false), reqBody, false)
	if err != nil {
		return resp, nil, latency, err
	}

	resp, err = upstreampkg.UnmarshalProtocolResponse(provCfg.Protocol, body)
	if err != nil {
		return resp, nil, latency, fmt.Errorf("unmarshal response: %w", err)
	}

	return resp, body, latency, nil
}
