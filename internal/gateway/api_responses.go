package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sower-proxy/deferlog/v2"
	"github.com/wweir/warden/config"
	bridgepkg "github.com/wweir/warden/internal/gateway/bridge"
	inferencepkg "github.com/wweir/warden/internal/gateway/inference"
	observepkg "github.com/wweir/warden/internal/gateway/observe"
	proxypkg "github.com/wweir/warden/internal/gateway/proxy"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/openai"
	"github.com/wweir/warden/pkg/toolhook"
)

// handleResponses handles Responses API requests (POST /*/responses).
func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses", "route", route.Prefix) }()
	hookGateway := g.hookGatewayTarget()

	var bootstrap inferenceBootstrap
	bootstrap, err = bootstrapInferenceRequest(r, route)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = bootstrap.request
	req := bootstrap.req
	serviceProtocol := proxypkg.ResponsesRequestProtocol(req.RawBody)
	stateful := serviceProtocol == config.RouteProtocolResponsesStateful
	if !route.SupportsServiceProtocol(serviceProtocol) {
		http.Error(w, proxypkg.UnsupportedResponsesProtocolMessage(route.ConfiguredProtocol(), serviceProtocol), http.StatusBadRequest)
		return
	}

	manager, ok := g.buildInferenceManager(w, route, serviceProtocol, "responses", req, req.ExplicitProvider == "" && !stateful)
	if !ok {
		return
	}
	applyRouteModelPrompt(&req, manager.Current().Model, openai.InjectSystemPromptResponsesRaw)

	for {
		current := manager.Current()
		if serviceProtocol == config.RouteProtocolResponsesStateless && current.Provider.ResponsesToChat && current.Provider.Protocol == "openai" {
			g.handleResponsesViaChat(w, r, route, req.RawBody, req.Model, req.Stream, manager, bootstrap.startTime, bootstrap.requestID)
			return
		}

		if g.handleBufferedInference(w, r, route, req, manager, bootstrap.startTime, bootstrap.requestID, bufferedInferenceSpec{
			serviceProtocol: serviceProtocol,
			endpoint:        "responses",
			canHandle: func(provider *config.ProviderConfig) bool {
				return !(serviceProtocol == config.RouteProtocolResponsesStateless && provider.ResponsesToChat && provider.Protocol == "openai")
			},
			upstreamPath: func(providerProtocol string) string {
				return upstreampkg.ProtocolEndpoint(providerProtocol, true)
			},
			prepareBody: func(_ string, rawBody []byte) ([]byte, error) {
				return rawBody, nil
			},
			runToolHooks: func(ctx context.Context, _ string, respBody []byte, stream bool) ([]byte, []toolhook.HookVerdict, asyncHookFn) {
				calls := observepkg.ParseResponsesToolCalls(respBody, stream)
				if stream {
					return respBody, nil, func(emit func([]toolhook.HookVerdict)) {
						if emit == nil {
							return
						}
						go func() {
							emit(observepkg.RunDegradedAsyncToolHooks(ctx, hookGateway, calls))
						}()
					}
				}

				blockVerdicts := observepkg.RunBlockToolHooks(ctx, hookGateway, calls)
				respBody = observepkg.InjectResponsesBlockVerdicts(respBody, blockVerdicts)
				return respBody, blockVerdicts, func(emit func([]toolhook.HookVerdict)) {
					if emit == nil {
						return
					}
					go func() {
						emit(observepkg.RunAsyncToolHooks(ctx, hookGateway, calls))
					}()
				}
			},
			writeStream: func(w http.ResponseWriter, _ string, respBody []byte) {
				writeEventStreamHeaders(w)
				writeStreamResponse(w, respBody, "Failed to write responses stream")
			},
			writeNonStream: func(w http.ResponseWriter, respBody []byte) {
				writeJSONResponse(w, respBody, "Failed to write responses response")
			},
			streamAssembler: func(string) observepkg.StreamLogAssembler {
				return func(respBody []byte) ([]byte, []byte, error) {
					assembled, err := openai.AssembleResponsesStream(respBody)
					return assembled, respBody, err
				}
			},
		}) {
			return
		}
	}
}

// handleResponsesViaChat handles Responses API requests by converting to/from Chat Completions.
// This is used when responses_to_chat is enabled for a provider.
func (g *Gateway) handleResponsesViaChat(w http.ResponseWriter, r *http.Request, route *config.RouteConfig,
	rawReqBody []byte, model string, stream bool, manager *inferencepkg.Manager, startTime time.Time, reqID string,
) {
	hookGateway := g.hookGatewayTarget()
	g.handleChatBridge(w, r, route, rawReqBody, model, stream, manager, startTime, reqID, chatBridgeSpec{
		serviceProtocol:   config.RouteProtocolResponsesStateless,
		endpoint:          "responses",
		streamWarn:        "ResponsesToChat stream terminated early",
		writeResponseWarn: "Failed to write converted response",
		buildChatRequest: func(rawReqBody []byte) (openai.ChatCompletionRequest, string, error) {
			var respReq openai.ResponsesRequest
			if err := json.Unmarshal(rawReqBody, &respReq); err != nil {
				return openai.ChatCompletionRequest{}, "", err
			}
			chatReq, err := openai.ResponsesRequestToChatRequest(respReq)
			if err != nil {
				return openai.ChatCompletionRequest{}, "", err
			}
			return chatReq, chatReq.Model, nil
		},
		streamRelay: func(src io.Reader, dst http.ResponseWriter, publicModel string) ([]byte, []byte, error) {
			return bridgepkg.StreamChatAsResponses(src, dst, publicModel)
		},
		streamLogAssembler: func(respBody []byte) ([]byte, []byte, error) {
			assembled, err := openai.AssembleResponsesStream(respBody)
			return assembled, respBody, err
		},
		runNonStreamToolHooks: func(ctx context.Context, chatResp openai.ChatCompletionResponse) ([]toolhook.HookVerdict, asyncHookFn) {
			calls := observepkg.ChatToolCalls(chatResp)
			blockVerdicts := observepkg.RunBlockToolHooks(ctx, hookGateway, calls)
			return blockVerdicts, func(emit func([]toolhook.HookVerdict)) {
				if emit == nil {
					return
				}
				go func() {
					emit(observepkg.RunAsyncToolHooks(ctx, hookGateway, calls))
				}()
			}
		},
		injectBlockVerdicts: observepkg.InjectResponsesBlockVerdicts,
		convertNonStreamResponse: func(chatResp openai.ChatCompletionResponse, publicModel string) ([]byte, error) {
			respResp, err := openai.ChatResponseToResponsesResponse(chatResp, publicModel)
			if err != nil {
				return nil, err
			}
			return json.Marshal(respResp)
		},
		writeConvertResponseError: func(w http.ResponseWriter, err error) {
			http.Error(w, fmt.Sprintf("convert response: %v", err), http.StatusInternalServerError)
		},
	})
}
