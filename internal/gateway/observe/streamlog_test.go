package observe

import (
	"testing"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
)

func TestAssembleResponseConvertsAnthropicChatStream(t *testing.T) {
	t.Parallel()

	const streamBody = "" +
		"event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_abc123\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-3-5-sonnet\",\"content\":[],\"stop_reason\":null,\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n\n"

	logged := AssembleResponse(config.RouteProtocolChat, config.RouteProtocolAnthropic, []byte(streamBody))
	if !gjson.ValidBytes(logged) {
		t.Fatalf("logged response is not valid JSON: %q", string(logged))
	}
	if got := gjson.GetBytes(logged, "object").String(); got != "chat.completion" {
		t.Fatalf("logged object = %q, want chat.completion", got)
	}
	if got := gjson.GetBytes(logged, "choices.0.message.content").String(); got != "Hello world" {
		t.Fatalf("logged content = %q, want Hello world", got)
	}
	if got := gjson.GetBytes(logged, "choices.0.finish_reason").String(); got != "stop" {
		t.Fatalf("logged finish_reason = %q, want stop", got)
	}
	if got := gjson.GetBytes(logged, "usage.prompt_tokens").Int(); got != 10 {
		t.Fatalf("logged prompt tokens = %d, want 10", got)
	}
	if got := gjson.GetBytes(logged, "usage.completion_tokens").Int(); got != 5 {
		t.Fatalf("logged completion tokens = %d, want 5", got)
	}
}

func TestMarshalRawStreamForLog(t *testing.T) {
	t.Parallel()

	got := string(MarshalRawStreamForLog([]byte(" data: partial\n\n ")))
	want := `"data: partial"`
	if got != want {
		t.Fatalf("MarshalRawStreamForLog() = %q, want %q", got, want)
	}
}
