package inference

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

func IsInferenceEndpoint(path string) bool {
	return strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/responses") ||
		strings.HasSuffix(path, "/embeddings") ||
		strings.HasSuffix(path, "/messages")
}

// ServiceProtocolFromRequest maps a request path to its service protocol. The
// stateless/stateful distinction was dropped, so /responses uniformly maps to
// RouteProtocolResponses; the gateway later inspects the request body to
// decide between the inference pipeline and transparent forwarding.
func ServiceProtocolFromRequest(path string) string {
	switch {
	case strings.HasSuffix(path, "/messages"):
		return config.RouteProtocolAnthropic
	case strings.HasSuffix(path, "/chat/completions"):
		return config.RouteProtocolChat
	case strings.HasSuffix(path, "/responses"):
		return config.RouteProtocolResponses
	case strings.HasSuffix(path, "/embeddings"):
		return config.ServiceProtocolEmbeddings
	default:
		return ""
	}
}

func UnsupportedRouteProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolChat:
		return "route protocol " + routeProtocol + " does not support chat requests"
	case config.RouteProtocolResponses:
		return "route protocol " + routeProtocol + " does not support responses requests"
	case config.RouteProtocolAnthropic:
		return "route protocol " + routeProtocol + " does not support anthropic messages requests"
	case config.ServiceProtocolEmbeddings:
		return "route protocol " + routeProtocol + " does not support embeddings requests"
	default:
		return "route does not support this request protocol"
	}
}

// IsStatefulResponsesRequest reports whether a Responses API request body
// carries a previous_response_id, indicating it is part of an ongoing
// server-tracked conversation. Such requests are forwarded transparently
// because Warden does not own the conversation state.
func IsStatefulResponsesRequest(rawReqBody []byte) bool {
	return gjson.GetBytes(rawReqBody, "previous_response_id").String() != ""
}
