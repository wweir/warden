package bridge

import (
	"bufio"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wweir/warden/pkg/protocol"
)

func TestRelayRawStreamAcceptsFinalChatChunkWithoutDoneSentinel(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		"data: {\"id\":\"chatcmpl_123\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n",
	)
	rec := httptest.NewRecorder()

	raw, err := RelayRawStream(stream, rec)
	if err != nil {
		t.Fatalf("RelayRawStream() error = %v", err)
	}
	if string(raw) != rec.Body.String() {
		t.Fatalf("relayed body = %q, raw = %q", rec.Body.String(), string(raw))
	}
}

func TestRelayRawStreamRejectsIncompleteChatChunk(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		"data: {\"id\":\"chatcmpl_123\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n",
	)
	rec := httptest.NewRecorder()

	_, err := RelayRawStream(stream, rec)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("RelayRawStream() error = %v, want unexpected EOF", err)
	}
}

func TestRelayAnthropicStreamAcceptsMessageStop(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	)
	rec := httptest.NewRecorder()

	raw, err := RelayAnthropicStream(stream, rec)
	if err != nil {
		t.Fatalf("RelayAnthropicStream() error = %v", err)
	}
	if string(raw) != rec.Body.String() {
		t.Fatalf("relayed body = %q, raw = %q", rec.Body.String(), string(raw))
	}
	if !strings.Contains(rec.Body.String(), "message_stop") {
		t.Fatal("relayed body missing message_stop event")
	}
}

func TestRelayAnthropicStreamRejectsIncomplete(t *testing.T) {
	t.Parallel()

	stream := strings.NewReader(
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n",
	)
	rec := httptest.NewRecorder()

	_, err := RelayAnthropicStream(stream, rec)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("RelayAnthropicStream() error = %v, want unexpected EOF", err)
	}
}

func TestStreamChatAsAnthropicBasic(t *testing.T) {
	t.Parallel()

	chatSSE := strings.NewReader(
		"data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chat-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	)
	rec := httptest.NewRecorder()

	rawChat, rawAnthropic, err := StreamChatAsAnthropic(chatSSE, rec)
	if err != nil {
		t.Fatalf("StreamChatAsAnthropic() error = %v", err)
	}
	_ = rawChat

	if len(rawAnthropic) == 0 {
		t.Fatal("converted anthropic stream is empty")
	}
	body := string(rawAnthropic)
	if !strings.Contains(body, "message_start") {
		t.Fatal("missing message_start event")
	}
	if !strings.Contains(body, "text_delta") {
		t.Fatal("missing text_delta event")
	}
	if !strings.Contains(body, "message_stop") {
		t.Fatal("missing message_stop event")
	}
}

func TestStreamChatAsAnthropicWithToolCalls(t *testing.T) {
	t.Parallel()

	chatSSE := strings.NewReader(
		"data: {\"id\":\"chat-tool\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chat-tool\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\"\"}}]},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chat-tool\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
			"data: [DONE]\n\n",
	)
	rec := httptest.NewRecorder()

	_, rawAnthropic, err := StreamChatAsAnthropic(chatSSE, rec)
	if err != nil {
		t.Fatalf("StreamChatAsAnthropic() with tool calls error = %v", err)
	}

	body := string(rawAnthropic)
	if !strings.Contains(body, "tool_use") {
		t.Fatal("converted anthropic stream missing tool_use")
	}
	if !strings.Contains(body, "get_weather") {
		t.Fatal("converted anthropic stream missing function name")
	}
}

func TestStreamChatAsResponsesBasic(t *testing.T) {
	t.Parallel()

	chatSSE := strings.NewReader(
		"data: {\"id\":\"resp-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"resp-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"resp-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n",
	)
	rec := httptest.NewRecorder()

	rawChat, rawResp, err := StreamChatAsResponses(chatSSE, rec, "test-model")
	if err != nil {
		t.Fatalf("StreamChatAsResponses() error = %v", err)
	}
	_ = rawChat

	if len(rawResp) == 0 {
		t.Fatal("converted responses stream is empty")
	}
	body := string(rawResp)
	if !strings.Contains(body, "response.created") {
		t.Fatal("missing response.created event")
	}
	if !strings.Contains(body, "output_text.delta") {
		t.Fatal("missing output_text.delta event")
	}
	if !strings.Contains(body, "response.completed") {
		t.Fatal("missing response.completed event")
	}
}

func TestStreamChatAsResponsesIncomplete(t *testing.T) {
	t.Parallel()

	chatSSE := strings.NewReader(
		"data: {\"id\":\"resp-inc\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"},\"finish_reason\":null}]}\n\n",
	)
	rec := httptest.NewRecorder()

	_, _, err := StreamChatAsResponses(chatSSE, rec, "test-model")
	if err == nil {
		t.Fatal("StreamChatAsResponses() expected error for incomplete stream, got nil")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "response.completed") {
		t.Fatal("missing response.completed for incomplete stream")
	}
	if !strings.Contains(body, "incomplete") {
		t.Fatal("completed event should have 'incomplete' status")
	}
}

func TestStreamAnthropicAsResponses(t *testing.T) {
	t.Parallel()

	anthropicSSE := strings.NewReader(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n" +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	)
	rec := httptest.NewRecorder()

	rawAnthropic, rawResp, err := StreamAnthropicAsResponses(anthropicSSE, rec, "claude-model")
	if err != nil {
		t.Fatalf("StreamAnthropicAsResponses() error = %v", err)
	}

	if len(rawAnthropic) == 0 {
		t.Fatal("rawAnthropic bytes should not be empty")
	}
	if len(rawResp) == 0 {
		t.Fatal("converted responses stream is empty")
	}
	body := string(rawResp)
	if !strings.Contains(body, "response.created") {
		t.Fatal("missing response.created event")
	}
	if !strings.Contains(body, "response.completed") {
		t.Fatal("missing response.completed event")
	}
}

func TestStreamAnthropicAsResponsesExceedsLimit(t *testing.T) {
	// Temporarily lower the limit so we don't need to allocate 64 MiB.
	oldLimit := maxBufferedStreamBody
	maxBufferedStreamBody = 1024
	defer func() { maxBufferedStreamBody = oldLimit }()

	t.Parallel()

	prefix := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\""
	suffix := "\"}}\n\n"
	pad := strings.Repeat("x", int(maxBufferedStreamBody)-len(prefix)-len(suffix)+1)
	stream := strings.NewReader(prefix + pad + suffix)

	rec := httptest.NewRecorder()

	_, _, err := StreamAnthropicAsResponses(stream, rec, "test")
	if err == nil {
		t.Fatal("expected error for stream exceeding limit")
	}
}

func TestReadSSEFrameMultiLineData(t *testing.T) {
	t.Parallel()

	input := "data: line1\ndata: line2\n\n"
	r := bufio.NewReader(strings.NewReader(input))

	frame, err := ReadSSEFrame(r)
	if err != nil {
		t.Fatalf("ReadSSEFrame() error = %v", err)
	}
	got := string(frame)
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line2") {
		t.Fatalf("ReadSSEFrame() = %q, want multi-line data", got)
	}
}

func TestReadSSEFrameWithEventType(t *testing.T) {
	t.Parallel()

	input := "event: test_event\ndata: {\"x\":1}\n\n"
	r := bufio.NewReader(strings.NewReader(input))

	frame, err := ReadSSEFrame(r)
	if err != nil {
		t.Fatalf("ReadSSEFrame() error = %v", err)
	}
	if !strings.Contains(string(frame), "test_event") {
		t.Fatal("frame missing event type")
	}
}

func TestAnthropicMessageStopEventIgnoresEmptyData(t *testing.T) {
	t.Parallel()

	evt := protocol.Event{Data: ""}
	if anthropicMessageStopEvent(evt) {
		t.Fatal("empty data should not signal completion")
	}
}

func TestAnthropicMessageStopEventDetectsStop(t *testing.T) {
	t.Parallel()

	evt := protocol.Event{Data: "{\"type\":\"message_stop\"}"}
	if !anthropicMessageStopEvent(evt) {
		t.Fatal("message_stop type should signal completion")
	}
}
