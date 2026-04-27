package observe

import (
	"encoding/json"
	"strings"

	"github.com/wweir/warden/config"
	upstreampkg "github.com/wweir/warden/internal/gateway/upstream"
	"github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

func AssembleResponse(serviceProtocol, upstreamProtocol string, body []byte) []byte {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "event:") && !strings.HasPrefix(trimmed, "data:") {
		return body
	}
	if serviceProtocol == config.RouteProtocolAnthropic {
		if assembled := anthropic.AssembleStream(body); assembled != nil {
			return assembled
		}
		return MarshalRawStreamForLog(body)
	}
	if config.IsResponsesRouteProtocol(serviceProtocol) {
		if assembled, err := openai.AssembleResponsesStream(body); err == nil {
			return assembled
		}
		return MarshalRawStreamForLog(body)
	}
	clientBody := upstreampkg.ConvertStreamIfNeeded(upstreamProtocol, body)
	if assembled, err := openai.AssembleChatStream(clientBody); err == nil {
		return assembled
	}
	return MarshalRawStreamForLog(clientBody)
}

func MarshalRawStreamForLog(body []byte) []byte {
	data, err := json.Marshal(strings.TrimSpace(string(body)))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return data
}
