package gateway

import (
	"bytes"
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
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/openai"
	"github.com/wweir/warden/pkg/toolhook"
)

// handleResponses handles Responses API requests (POST /*/responses).
//
// Stateless requests (no previous_response_id) run through the inference
// pipeline, which supports failover, tool hooks, and responses_to_chat
// bridging. Stateful requests (carrying previous_response_id) are forwarded
// transparently because the upstream owns the conversation state and Warden
// cannot safely fail over mid-session.
func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request, route *config.RouteConfig) {
	var err error
	defer func() { deferlog.DebugError(err, "handle responses", "route", route.Prefix) }()

	var bootstrap inferenceBootstrap
	bootstrap, err = bootstrapInferenceRequest(r, route)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r = bootstrap.request
	req := bootstrap.req
	serviceProtocol := config.RouteProtocolResponses
	if !route.SupportsServiceProtocol(serviceProtocol) {
		http.Error(w, inferencepkg.UnsupportedRouteProtocolMessage(route.ConfiguredProtocol(), serviceProtocol), http.StatusBadRequest)
		return
	}

	isStateful := inferencepkg.IsStatefulResponsesRequest(req.RawBody)
	manager, ok := g.buildInferenceManager(w, route, serviceProtocol, "responses", req, !isStateful && req.ExplicitProvider == "")
	if !ok {
		return
	}

	// providerNeedsChatBridge reports whether the current provider must serve
	// Responses requests through the chat bridge instead of native /responses.
	providerNeedsChatBridge := func(provider *config.ProviderConfig, accessMode string) bool {
		return accessMode == string(config.ProviderFormatOpenAI) && config.FormatHasBridge(provider, config.ProviderFormatOpenAI, "responses_to_chat")
	}
	// providerNeedsMessagesBridge reports whether the current provider must
	// serve Responses requests by translating through upstream Anthropic
	// /messages instead of native /responses. Stateful requests cannot use
	// this bridge because Anthropic does not own conversation state.
	providerNeedsMessagesBridge := func(provider *config.ProviderConfig, accessMode string) bool {
		return accessMode == string(config.ProviderFormatAnthropic) && config.FormatHasBridge(provider, config.ProviderFormatAnthropic, "anthropic_to_responses")
	}

	if isStateful {
		currentAccessMode := ""
		if currentTarget := manager.Current().Target; currentTarget != nil {
			currentAccessMode = currentTarget.Format
		}
		if providerNeedsMessagesBridge(manager.Current().Provider, currentAccessMode) {
			http.Error(w, "anthropic_to_responses provider does not support stateful responses requests", http.StatusBadRequest)
			return
		}
		g.forwardResponsesTransparently(w, r, route, req.RawBody)
		return
	}

	applyRouteModelPrompt(&req, manager.Current().Model, openai.InjectSystemPromptResponsesRaw)
	hookGateway := g.hookGatewayTarget()

	for {
		current := manager.Current().Provider
		currentAccessMode := ""
		if currentTarget := manager.Current().Target; currentTarget != nil {
			currentAccessMode = currentTarget.Format
		}
		if providerNeedsChatBridge(current, currentAccessMode) {
			if err := g.handleResponsesViaChat(w, r, route, req.RawBody, req.Model, req.Stream, manager, bootstrap.startTime, bootstrap.requestID); err != nil {
				if manager.HandleError(err) {
					continue
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			return
		}
		if providerNeedsMessagesBridge(current, currentAccessMode) {
			if err := g.handleResponsesViaMessages(w, r, route, req.RawBody, req.Model, req.Stream, manager, bootstrap.startTime, bootstrap.requestID); err != nil {
				if manager.HandleError(err) {
					continue
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			return
		}

		if g.handleInference(w, r, route, req, manager, bootstrap.startTime, bootstrap.requestID, inferenceSpec{
			serviceProtocol: serviceProtocol,
			endpoint:        "responses",
			canHandle: func(provider *config.ProviderConfig) bool {
				return !providerNeedsChatBridge(provider, currentAccessMode) && !providerNeedsMessagesBridge(provider, currentAccessMode)
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
			writeBufferedStream: func(w http.ResponseWriter, _ string, respBody []byte) {
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
			streamRelay:            bridgepkg.RelayRawStream,
			allowNonStreamFallback: true,
		}) {
			return
		}
	}
}

// forwardResponsesTransparently routes a stateful Responses request through
// the shared transparent proxy handler. The proxy path supports model
// rewriting (via the matched route target), provider auth/header injection,
// and request logging without parsing or rewriting the response body.
func (g *Gateway) forwardResponsesTransparently(w http.ResponseWriter, r *http.Request, route *config.RouteConfig, rawBody []byte) {
	// Replay the buffered body so the proxy handler can re-read it; the
	// inference bootstrap above consumed the original stream.
	r.Body = io.NopCloser(bytes.NewReader(rawBody))
	r.ContentLength = int64(len(rawBody))
	// Strip the route prefix so the proxy handler concatenates only the
	// upstream-facing tail (e.g. /responses) onto provider URL. The route
	// context value installed by bootstrapInferenceRequest is preserved
	// because trimRoutePrefix clones with the same context.
	g.proxyHandler().Handle(w, trimRoutePrefix(r, route.Prefix), route)
}

// handleResponsesViaChat handles Responses API requests by translating
// through upstream Chat Completions. Used when a provider enables
// responses_to_chat (family=openai).
func (g *Gateway) handleResponsesViaChat(w http.ResponseWriter, r *http.Request, route *config.RouteConfig,
	rawReqBody []byte, model string, stream bool, manager *inferencepkg.Manager, startTime time.Time, reqID string,
) error {
	return g.handleChatBridge(w, r, route, rawReqBody, model, stream, manager, startTime, reqID,
		g.responsesBridgeSpec(bridgepkg.StreamChatAsResponses, "ResponsesToChat stream terminated early"))
}

// handleResponsesViaMessages handles Responses API requests by translating
// through upstream Anthropic Messages. Used when a provider enables
// anthropic_to_responses (family=anthropic). Anthropic does not expose a
// stateful streaming converter, so the upstream SSE is buffered first and
// then refolded through Chat IR into Responses SSE.
func (g *Gateway) handleResponsesViaMessages(w http.ResponseWriter, r *http.Request, route *config.RouteConfig,
	rawReqBody []byte, model string, stream bool, manager *inferencepkg.Manager, startTime time.Time, reqID string,
) error {
	return g.handleChatBridge(w, r, route, rawReqBody, model, stream, manager, startTime, reqID,
		g.responsesBridgeSpec(bridgepkg.StreamAnthropicAsResponses, "AnthropicToResponses stream terminated early"))
}

// responsesBridgeSpec is the shared chatBridgeSpec for both Responses bridges:
// the only per-upstream knobs are streamRelay (SSE conversion driver) and the
// streamWarn log message; the rest is identical because both bridges share the
// same Responses request/response shape and rely on the upstream adapter to
// marshal the Chat IR into the upstream's native protocol.
func (g *Gateway) responsesBridgeSpec(
	streamRelay func(src io.Reader, dst http.ResponseWriter, publicModel string) ([]byte, []byte, error),
	streamWarn string,
) chatBridgeSpec {
	hookGateway := g.hookGatewayTarget()
	return chatBridgeSpec{
		serviceProtocol:   config.RouteProtocolResponses,
		endpoint:          "responses",
		streamWarn:        streamWarn,
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
		streamRelay: streamRelay,
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
	}
}
