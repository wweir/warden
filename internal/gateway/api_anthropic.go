package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	bridgepkg "github.com/wweir/warden/internal/gateway/bridge"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// handleAnthropicMessages handles Anthropic Messages API requests.
func (g *Gateway) handleAnthropicMessages(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle anthropic messages", "route", route.Prefix) }()

	var bootstrap inferenceBootstrap
	bootstrap, err = bootstrapInferenceRequest(r, route)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = bootstrap.request
	req := bootstrap.req

	manager, ok := g.buildInferenceManager(w, route, config.RouteProtocolAnthropic, "messages", req, req.ExplicitProvider == "")
	if !ok {
		return
	}

	for {
		current := manager.Current()
		if current.Provider.Protocol == config.ProviderProtocolOpenAI && current.Provider.AnthropicToChat {
			g.handleAnthropicMessagesViaChat(w, r, route, prepareRawBody(req.RawBody, current.Target), req.Model, req.Stream, manager, bootstrap.startTime, bootstrap.requestID)
			return
		}
		if g.handleRelayInference(w, r, route, req, manager, bootstrap.startTime, bootstrap.requestID, relayInferenceSpec{
			serviceProtocol: config.RouteProtocolAnthropic,
			endpoint:        "messages",
			streamWarn:      "Anthropic stream terminated early",
			canHandle: func(provider *config.ProviderConfig) bool {
				return !(provider.Protocol == config.ProviderProtocolOpenAI && provider.AnthropicToChat)
			},
			upstreamPath: func(providerProtocol string) string {
				return upstreampkg.ProtocolEndpoint(providerProtocol, false)
			},
			prepareBody: func(_ string, rawBody []byte) ([]byte, error) {
				return rawBody, nil
			},
			streamRelay:     bridgepkg.RelayAnthropicStream,
			streamAssembler: observepkg.AssembleAnthropicStreamLog,
			runToolHooks: func(ctx context.Context, providerProtocol string, respBody []byte, stream bool) {
				observepkg.RunRouteToolHooks(ctx, g.cfg.Addr, observepkg.ParseChatToolCalls(providerProtocol, respBody, stream), "Anthropic: failed to run tool hooks")
			},
			writeNonStream: func(w http.ResponseWriter, respBody []byte) {
				writeJSONResponse(w, respBody, "Failed to write anthropic response")
			},
		}) {
			return
		}
	}
}

func (g *Gateway) handleAnthropicMessagesViaChat(
	w http.ResponseWriter,
	r *http.Request,
	route *config.RouteConfig,
	rawReqBody []byte,
	model string,
	stream bool,
	manager *inferencepkg.Manager,
	startTime time.Time,
	reqID string,
) {
	g.handleChatBridge(w, r, route, rawReqBody, model, stream, manager, startTime, reqID, chatBridgeSpec{
		serviceProtocol:   config.RouteProtocolAnthropic,
		endpoint:          "messages",
		streamWarn:        "AnthropicToChat stream terminated early",
		streamToolHookOp:  "AnthropicToChat stream: failed to run tool hooks",
		writeResponseWarn: "Failed to write anthropic bridge response",
		buildChatRequest: func(rawReqBody []byte) (openai.ChatCompletionRequest, string, error) {
			chatReq, err := anthropic.MessagesRequestToChatRequest(rawReqBody)
			if err != nil {
				return openai.ChatCompletionRequest{}, "", err
			}
			return chatReq, model, nil
		},
		streamRelay: func(src io.Reader, dst http.ResponseWriter, _ string) ([]byte, []byte, error) {
			return bridgepkg.StreamChatAsAnthropic(src, dst)
		},
		streamLogAssembler: observepkg.AssembleAnthropicStreamLog,
		runNonStreamToolHooks: func(ctx context.Context, chatResp openai.ChatCompletionResponse) {
			observepkg.RunFirstChoiceToolHooks(ctx, g.cfg.Addr, chatResp, "AnthropicToChat: failed to run tool hooks")
		},
		convertNonStreamResponse: func(chatResp openai.ChatCompletionResponse, _ string) ([]byte, error) {
			return anthropic.ChatResponseToMessagesResponse(chatResp)
		},
		writeConvertResponseError: func(w http.ResponseWriter, err error) {
			http.Error(w, fmt.Sprintf("convert chat response to anthropic: %v", err), http.StatusBadGateway)
		},
	})
}
