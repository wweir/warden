package inference

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

func IsInferenceEndpoint(path string) bool {
	if strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/embeddings") {
		return true
	}
	return path == "/messages" || strings.HasSuffix(path, "/messages")
}

func ServiceProtocolFromRequest(path string, reqBody []byte) string {
	if strings.HasSuffix(path, "/messages") || path == "/messages" {
		return config.RouteProtocolAnthropic
	}
	if strings.HasSuffix(path, "/chat/completions") {
		return config.RouteProtocolChat
	}
	if strings.HasSuffix(path, "/responses") {
		return ResponsesRequestProtocol(reqBody)
	}
	if strings.HasSuffix(path, "/embeddings") {
		return config.ServiceProtocolEmbeddings
	}
	return ""
}

func UnsupportedRouteProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolResponsesStateful:
		return UnsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol)
	case config.RouteProtocolResponsesStateless:
		return UnsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol)
	case config.RouteProtocolChat:
		return "route protocol " + routeProtocol + " does not support chat requests"
	case config.RouteProtocolAnthropic:
		return "route protocol " + routeProtocol + " does not support anthropic messages requests"
	case config.ServiceProtocolEmbeddings:
		return "route protocol " + routeProtocol + " does not support embeddings requests"
	default:
		return "route does not support this request protocol"
	}
}

func ResponsesRequestProtocol(rawReqBody []byte) string {
	if gjson.GetBytes(rawReqBody, "previous_response_id").String() != "" {
		return config.RouteProtocolResponsesStateful
	}
	return config.RouteProtocolResponsesStateless
}

func UnsupportedResponsesProtocolMessage(routeProtocol, serviceProtocol string) string {
	switch serviceProtocol {
	case config.RouteProtocolResponsesStateful:
		return fmt.Sprintf("route protocol %s does not support stateful responses requests", routeProtocol)
	case config.RouteProtocolResponsesStateless:
		return fmt.Sprintf("route protocol %s does not support stateless responses requests", routeProtocol)
	default:
		return fmt.Sprintf("route protocol %s does not support responses requests", routeProtocol)
	}
}
