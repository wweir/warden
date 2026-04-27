package bridge

import (
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
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
