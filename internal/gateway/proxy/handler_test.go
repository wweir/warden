package proxy

import (
	"testing"

	"github.com/wweir/warden/config"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
)

func TestObserveProxyTokenUsageEstimatesFromAssembledStreamLog(t *testing.T) {
	reqBody := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	streamBody := []byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n")
	logResp := []byte(`{"id":"chatcmpl_1","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)

	obs := observeProxyTokenUsage(true, config.RouteProtocolChat, config.ProviderFormatOpenAI, streamBody, logResp, reqBody, "gpt-4o")

	if obs.SourceLabel() != tokenusagepkg.SourceEstimated {
		t.Fatalf("source = %q, want %q", obs.SourceLabel(), tokenusagepkg.SourceEstimated)
	}
	if obs.PromptTokens == 0 {
		t.Fatalf("expected prompt tokens, got %+v", obs)
	}
	if obs.CompletionTokens == 0 {
		t.Fatalf("expected completion tokens from assembled log response, got %+v", obs)
	}
}
