package gateway

import (
	"encoding/json"

	sel "github.com/wweir/warden/internal/selector"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// protocolEndpoint returns the upstream API endpoint for a given protocol and API type.
func protocolEndpoint(protocol string, isResponses bool) string {
	if isResponses {
		return "/responses"
	}
	switch protocol {
	case "anthropic":
		return anthropic.Endpoint
	default:
		return "/chat/completions"
	}
}

// marshalProtocolRequest marshals a ChatCompletionRequest for the given protocol.
// For Anthropic, it converts from OpenAI format to Anthropic Messages API format.
// For OpenAI and Ollama, it marshals directly.
func marshalProtocolRequest(protocol string, req openai.ChatCompletionRequest) ([]byte, error) {
	switch protocol {
	case "anthropic":
		return anthropic.MarshalRequest(req)
	default:
		return json.Marshal(req)
	}
}

// rewriteModelRaw replaces the "model" field in raw JSON request bytes when needed.
func rewriteModelRaw(rawBody []byte, model string) []byte {
	if model == "" {
		return rawBody
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return rawBody
	}

	body["model"], _ = json.Marshal(model)
	result, err := json.Marshal(body)
	if err != nil {
		return rawBody
	}
	return result
}

// prepareRawBody rewrites the public route model to the selected upstream model.
func prepareRawBody(rawBody []byte, target *sel.RouteTarget) []byte {
	if target != nil {
		rawBody = rewriteModelRaw(rawBody, target.UpstreamModel)
	}
	return rawBody
}

// marshalProtocolRaw converts raw OpenAI-format request bytes to the target protocol.
// For OpenAI/Ollama, the bytes are passed through as-is (no re-serialization).
// For Anthropic, a full decode→convert→encode is required.
func marshalProtocolRaw(protocol string, rawBody []byte) ([]byte, error) {
	switch protocol {
	case "anthropic":
		var req openai.ChatCompletionRequest
		if err := json.Unmarshal(rawBody, &req); err != nil {
			return nil, err
		}
		return anthropic.MarshalRequest(req)
	default:
		return rawBody, nil
	}
}

// unmarshalProtocolResponse unmarshals a response body into ChatCompletionResponse
// for the given protocol. Anthropic responses are converted to OpenAI format.
func unmarshalProtocolResponse(protocol string, body []byte) (openai.ChatCompletionResponse, error) {
	switch protocol {
	case "anthropic":
		return anthropic.UnmarshalResponse(body)
	default:
		var resp openai.ChatCompletionResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return resp, err
		}
		return resp, nil
	}
}

// newStreamParser creates a StreamParser based on protocol and API type.
func newStreamParser(protocolType string, isResponses bool) protocol.StreamParser {
	if isResponses {
		return &openai.ResponsesStreamParser{}
	}
	switch protocolType {
	case "anthropic":
		return &anthropic.StreamParser{}
	default:
		return &openai.ChatStreamParser{}
	}
}

// convertStreamIfNeeded converts Anthropic SSE to OpenAI SSE format.
// For non-Anthropic protocols, the raw bytes are returned as-is.
func convertStreamIfNeeded(protocol string, rawSSE []byte) []byte {
	if protocol == "anthropic" {
		return anthropic.ConvertStreamToOpenAI(rawSSE)
	}
	return rawSSE
}
