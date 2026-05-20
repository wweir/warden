package tokenusage

import (
	"errors"
	"testing"

	"github.com/tiktoken-go/tokenizer"
)

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

func TestFromJSONEmbeddingsUsage(t *testing.T) {
	t.Parallel()

	obs := FromEmbeddingsJSON([]byte(`{"usage":{"prompt_tokens":6,"total_tokens":6}}`))
	if obs.CompletenessLabel() != CompletenessExact {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessExact)
	}
	if obs.PromptTokens != 6 || obs.CompletionTokens != 0 || obs.TotalTokens != 6 {
		t.Fatalf("unexpected observation: %+v", obs)
	}
}

func TestFromJSONDoesNotInferEmbeddingsCompletionTokens(t *testing.T) {
	t.Parallel()

	obs := FromJSON([]byte(`{"usage":{"prompt_tokens":6,"total_tokens":6}}`))
	if obs.CompletenessLabel() != CompletenessPartial {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessPartial)
	}
	if obs.PromptTokens != 6 || obs.CompletionTokens != 0 || obs.TotalTokens != 6 {
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

func TestEstimateTokenUsageCountsResponsesInputArray(t *testing.T) {
	t.Parallel()

	obs := EstimateTokenUsage(
		[]byte(`{"model":"gpt-4o","input":[{"type":"message","role":"developer","content":"be precise"},{"type":"message","role":"user","content":[{"type":"input_text","text":"hello world"}]},{"type":"function_call_output","call_id":"call_1","output":{"ok":true}}]}`),
		"gpt-4o",
		"done",
	)
	if obs.CompletenessLabel() != CompletenessPartial {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessPartial)
	}
	if obs.PromptTokens == 0 {
		t.Fatalf("expected prompt tokens from responses input array, got %+v", obs)
	}
	if obs.CompletionTokens == 0 {
		t.Fatalf("expected completion tokens from output, got %+v", obs)
	}
}

func TestEstimateTokenUsageCountsResponsesInstructions(t *testing.T) {
	t.Parallel()

	obs := EstimateTokenUsage(
		[]byte(`{"model":"gpt-4o","instructions":"be precise","input":"hello"}`),
		"gpt-4o",
		"done",
	)
	if obs.PromptTokens == 0 {
		t.Fatalf("expected prompt tokens from instructions, got %+v", obs)
	}
}

func TestEstimateTokenUsageCountsToolCallFields(t *testing.T) {
	t.Parallel()

	obs := EstimateTokenUsage(
		[]byte(`{"model":"gpt-4o","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Hangzhou\"}"}}]}]}`),
		"gpt-4o",
		"",
	)
	if obs.PromptTokens == 0 {
		t.Fatalf("expected prompt tokens from tool call fields, got %+v", obs)
	}
	if obs.CompletenessLabel() != CompletenessPartial {
		t.Fatalf("completeness = %q, want %q", obs.CompletenessLabel(), CompletenessPartial)
	}
}

func TestEstimateTokenUsageDoesNotCountResponsesImageURLAsText(t *testing.T) {
	t.Parallel()

	obs := EstimateTokenUsage(
		[]byte(`{"model":"gpt-4o","input":[{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.test/image.png"}]}]}`),
		"gpt-4o",
		"done",
	)
	if obs.PromptTokens != 0 {
		t.Fatalf("prompt tokens = %d, want 0 for image-only text estimate", obs.PromptTokens)
	}
	if obs.CompletionTokens == 0 {
		t.Fatalf("expected completion tokens from output, got %+v", obs)
	}
}

func TestExtractOutputTextSupportsResponsesSSE(t *testing.T) {
	t.Parallel()

	raw := []byte("event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hel\"}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"lo\"}\n\n")

	if got := ExtractOutputText(raw); got != "hello" {
		t.Fatalf("ExtractOutputText = %q, want %q", got, "hello")
	}
}

type fakeCodec struct {
	name string
}

func (c fakeCodec) GetName() string { return c.name }
func (c fakeCodec) Count(string) (int, error) {
	return 0, nil
}
func (c fakeCodec) Encode(string) ([]uint, []string, error) {
	return nil, nil, nil
}
func (c fakeCodec) Decode([]uint) (string, error) {
	return "", nil
}

func TestCodecForModelPrefersExactTokenizerLookup(t *testing.T) {
	t.Parallel()

	codecCache.once.Do(initCodecCache)
	got := codecForModelWithLookup("qwen3.5:9b", func(model tokenizer.Model) (tokenizer.Codec, error) {
		if model == "qwen3.5:9b" {
			return fakeCodec{name: "exact-model"}, nil
		}
		return nil, errors.New("unexpected model")
	})
	if got == nil || got.GetName() != "exact-model" {
		t.Fatalf("codecForModelWithLookup() = %v, want exact-model", got)
	}
}

func TestCodecForModelFallsBackToFamilyRules(t *testing.T) {
	t.Parallel()

	codecCache.once.Do(initCodecCache)
	got := codecForModelWithLookup("gpt-5.4", func(tokenizer.Model) (tokenizer.Codec, error) {
		return nil, tokenizer.ErrModelNotSupported
	})
	if got == nil || got.GetName() != "o200k_base" {
		t.Fatalf("codecForModelWithLookup(gpt-5.4) = %v, want o200k_base", got)
	}
}
