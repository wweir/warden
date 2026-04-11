package tokenusage

import "testing"

func TestFromJSONOpenAIUsage(t *testing.T) {
	t.Parallel()

	obs := FromJSON([]byte(`{"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8,"prompt_tokens_details":{"cached_tokens":2}}}`))
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 3 || obs.CompletionTokens != 5 || obs.CacheTokens != 2 || obs.TotalTokens != 8 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromJSONResponsesUsage(t *testing.T) {
	t.Parallel()

	obs := FromJSON([]byte(`{"usage":{"input_tokens":7,"output_tokens":11,"total_tokens":18,"input_tokens_details":{"cached_tokens":4}}}`))
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 7 || obs.CompletionTokens != 11 || obs.CacheTokens != 4 || obs.TotalTokens != 18 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromJSONAnthropicUsageWithCache(t *testing.T) {
	t.Parallel()

	obs := FromJSON([]byte(`{"usage":{"input_tokens":7,"output_tokens":11,"cache_creation_input_tokens":13,"cache_read_input_tokens":17}}`))
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 7 || obs.CompletionTokens != 11 || obs.CacheTokens != 30 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromOpenAIChatStream(t *testing.T) {
	t.Parallel()

	raw := []byte("data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":5,\"total_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":2}}}\n\n" +
		"data: [DONE]\n\n")

	obs := FromOpenAIChatStream(raw)
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 3 || obs.CompletionTokens != 5 || obs.CacheTokens != 2 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromResponsesStream(t *testing.T) {
	t.Parallel()

	raw := []byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":2,\"output_tokens\":4,\"total_tokens\":6,\"input_tokens_details\":{\"cached_tokens\":1}}}}\n\n")

	obs := FromResponsesStream(raw)
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 2 || obs.CompletionTokens != 4 || obs.CacheTokens != 1 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromAnthropicStream(t *testing.T) {
	t.Parallel()

	raw := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0,\"cache_creation_input_tokens\":2,\"cache_read_input_tokens\":3}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n")

	obs := FromAnthropicStream(raw)
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 10 || obs.CompletionTokens != 5 || obs.CacheTokens != 5 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromAnthropicStreamPartial(t *testing.T) {
	t.Parallel()

	raw := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")

	obs := FromAnthropicStream(raw)
	if obs.CompletenessLabel() != CompletenessPartial {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessPartial)
	}
	if obs.PromptTokens != 10 || obs.CompletionTokens != 0 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}
