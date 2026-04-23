package upstream

import (
	"encoding/json"

	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

// ProtocolEndpoint returns the upstream API endpoint for a given protocol and API type.
func ProtocolEndpoint(protocol string, isResponses bool) string {
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

func EmbeddingsEndpoint() string {
	return "/" + config.ServiceProtocolEmbeddings
}

// MarshalProtocolRequest marshals a ChatCompletionRequest for the given protocol.
func MarshalProtocolRequest(protocol string, req openai.ChatCompletionRequest) ([]byte, error) {
	switch protocol {
	case "anthropic":
		return anthropic.MarshalRequest(req)
	default:
		return json.Marshal(req)
	}
}

// RewriteModelRaw replaces the "model" field in raw JSON request bytes when needed.
func RewriteModelRaw(rawBody []byte, model string) []byte {
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

// MarshalProtocolRaw converts raw OpenAI-format request bytes to the target protocol.
func MarshalProtocolRaw(protocol string, rawBody []byte) ([]byte, error) {
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

// UnmarshalProtocolResponse unmarshals a response body into ChatCompletionResponse for the given protocol.
func UnmarshalProtocolResponse(protocol string, body []byte) (openai.ChatCompletionResponse, error) {
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

// NewStreamParser creates a StreamParser based on protocol and API type.
func NewStreamParser(protocolType string, isResponses bool) protocol.StreamParser {
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

// ConvertStreamIfNeeded converts Anthropic SSE to OpenAI SSE format.
func ConvertStreamIfNeeded(protocol string, rawSSE []byte) []byte {
	if protocol == "anthropic" {
		return anthropic.ConvertStreamToOpenAI(rawSSE)
	}
	return rawSSE
}
