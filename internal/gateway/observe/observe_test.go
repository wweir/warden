package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wweir/warden/config"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	telemetrypkg "github.com/wweir/warden/internal/gateway/telemetry"
	tokenusagepkg "github.com/wweir/warden/internal/gateway/tokenusage"
	"github.com/wweir/warden/internal/reqlog"
	"github.com/wweir/warden/pkg/protocol"
	"github.com/wweir/warden/pkg/toolhook"
)

func TestRunRouteToolHooks_PostHooksOutliveRequestCancellation(t *testing.T) {
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case called <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"allow":true}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = requestctxpkg.WithRouteHooks(ctx, []*config.HookRuleConfig{{
		Match: "filesystem__write_file",
		Hook: config.HookConfig{
			Type: "http",
			When: "async",
			WebhookCfg: &config.WebhookConfig{
				URL: server.URL,
			},
		},
	}})
	cancel()

	RunRouteToolHooks(ctx, toolhook.GatewayTarget{}, []protocol.ToolCallInfo{{
		ID:        "call_1",
		Name:      "filesystem__write_file",
		Arguments: `{"path":"/tmp/a"}`,
	}})

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("expected post hook to run after request context cancellation")
	}
}

func TestRunRouteToolHooksSkipsUnmatchedCallsInVerdicts(t *testing.T) {
	t.Parallel()

	ctx := requestctxpkg.WithRouteHooks(context.Background(), []*config.HookRuleConfig{{
		Match: "filesystem__write_file",
		Hook: config.HookConfig{
			Type: "exec",
			When: "block",
		},
	}})

	verdicts := RunBlockToolHooks(ctx, toolhook.GatewayTarget{}, []protocol.ToolCallInfo{{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"q":"hello"}`,
	}})

	if len(verdicts) != 0 {
		t.Fatalf("expected no verdicts for unmatched tool call, got %+v", verdicts)
	}
}

func TestRecordInferenceLogCarriesTTFT(t *testing.T) {
	t.Parallel()

	got := recordTestInferenceLogWithTTFT(true)

	if got.DurationMs != 456 {
		t.Fatalf("DurationMs = %d, want 456", got.DurationMs)
	}
	if got.TTFTMs == nil || *got.TTFTMs != 123 {
		t.Fatalf("TTFTMs = %v, want 123", got.TTFTMs)
	}
}

func TestRecordInferenceLogSkipsTTFTForNonStream(t *testing.T) {
	t.Parallel()

	got := recordTestInferenceLogWithTTFT(false)

	if got.TTFTMs != nil {
		t.Fatalf("TTFTMs = %v, want nil for non-stream request", got.TTFTMs)
	}
}

func recordTestInferenceLogWithTTFT(stream bool) reqlog.Record {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	params := NewInferenceLogParams(
		req,
		time.Unix(0, 0),
		"req_1",
		"/openai",
		"chat/completions",
		"gpt-4o",
		stream,
		[]byte(`{"model":"gpt-4o"}`),
		nil,
		telemetrypkg.Labels{},
		"openai",
	).WithTTFT(123 * time.Millisecond).WithDuration(456)

	var got reqlog.Record
	RecordInferenceLog(
		params,
		[]byte(`{"ok":true}`),
		"",
		nil,
		tokenusagepkg.Missing(""),
		nil,
		nil,
		func(record reqlog.Record) {
			got = record
		},
	)

	return got
}
