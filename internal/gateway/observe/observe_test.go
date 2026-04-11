package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wweir/warden/config"
	requestctxpkg "github.com/wweir/warden/internal/gateway/requestctx"
	"github.com/wweir/warden/pkg/protocol"
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

	RunRouteToolHooks(ctx, "", []protocol.ToolCallInfo{{
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

	verdicts := RunBlockToolHooks(ctx, "", []protocol.ToolCallInfo{{
		ID:        "call_1",
		Name:      "web_search",
		Arguments: `{"q":"hello"}`,
	}})

	if len(verdicts) != 0 {
		t.Fatalf("expected no verdicts for unmatched tool call, got %+v", verdicts)
	}
}
