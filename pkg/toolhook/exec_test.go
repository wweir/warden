package toolhook

import "testing"

func TestParseHookResponse(t *testing.T) {
	t.Run("reject on allow false", func(t *testing.T) {
		r := hookResult{}
		parseHookResponse(`{"allow":false,"reason":"blocked"}`, &r)
		if !r.rejected {
			t.Fatalf("expected rejected=true")
		}
		if r.reason != "blocked" {
			t.Fatalf("expected reason=blocked, got %s", r.reason)
		}
	})

	t.Run("extract json from wrapper text", func(t *testing.T) {
		r := hookResult{}
		parseHookResponse("answer: {\"allow\":false,\"reason\":\"bad\"}", &r)
		if !r.rejected {
			t.Fatalf("expected rejected=true")
		}
		if r.reason != "bad" {
			t.Fatalf("expected reason=bad, got %s", r.reason)
		}
	})

	t.Run("json without allow fails open", func(t *testing.T) {
		r := hookResult{}
		parseHookResponse(`{"reason":"missing allow"}`, &r)
		if r.rejected {
			t.Fatalf("expected rejected=false")
		}
		if r.reason != "" {
			t.Fatalf("expected empty reason, got %s", r.reason)
		}
	})

	t.Run("invalid output fail open", func(t *testing.T) {
		r := hookResult{}
		parseHookResponse("not-json", &r)
		if r.rejected {
			t.Fatalf("expected rejected=false")
		}
	})
}
