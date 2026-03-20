package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/wweir/warden/pkg/protocol"
	anthproto "github.com/wweir/warden/pkg/protocol/anthropic"
)

type streamRelaySource string

const (
	streamRelaySourceUpstream   streamRelaySource = "upstream"
	streamRelaySourceDownstream streamRelaySource = "downstream"
)

type streamRelayError struct {
	source streamRelaySource
	err    error
}

func (e *streamRelayError) Error() string {
	return e.err.Error()
}

func (e *streamRelayError) Unwrap() error {
	return e.err
}

func streamRelayErrorSource(err error) streamRelaySource {
	var relayErr *streamRelayError
	if errors.As(err, &relayErr) {
		return relayErr.source
	}
	return ""
}

func relayAnthropicStream(src io.Reader, dst http.ResponseWriter) ([]byte, error) {
	reader := bufio.NewReader(src)
	var raw bytes.Buffer
	streamComplete := false

	for {
		frame, err := readSSEFrame(reader)
		if len(frame) > 0 {
			raw.Write(frame)
			events := protocol.ParseEvents(frame)
			for _, evt := range events {
				if anthropicMessageStopEvent(evt) {
					streamComplete = true
				}
			}
			if _, writeErr := dst.Write(frame); writeErr != nil {
				return raw.Bytes(), &streamRelayError{source: streamRelaySourceDownstream, err: writeErr}
			}
			dst.(http.Flusher).Flush()
		}

		if err != nil {
			if err == io.EOF {
				if !streamComplete {
					return raw.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: io.ErrUnexpectedEOF}
				}
				return raw.Bytes(), nil
			}
			return raw.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: err}
		}
	}
}

func streamChatAsAnthropic(src io.Reader, dst http.ResponseWriter) ([]byte, []byte, error) {
	reader := bufio.NewReader(src)
	state := anthproto.NewChatToMessagesStreamState()
	var rawChat bytes.Buffer
	var rawAnthropic bytes.Buffer
	streamComplete := false

	for {
		frame, err := readSSEFrame(reader)
		if len(frame) > 0 {
			rawChat.Write(frame)
			events := protocol.ParseEvents(frame)
			for _, evt := range events {
				if evt.Data == "[DONE]" {
					streamComplete = true
					continue
				}
				converted, convErr := state.ConvertEvent(evt)
				if convErr != nil {
					return rawChat.Bytes(), rawAnthropic.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: convErr}
				}
				if len(converted) == 0 {
					continue
				}
				rawAnthropic.Write(converted)
				if _, writeErr := dst.Write(converted); writeErr != nil {
					return rawChat.Bytes(), rawAnthropic.Bytes(), &streamRelayError{source: streamRelaySourceDownstream, err: writeErr}
				}
				dst.(http.Flusher).Flush()
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return rawChat.Bytes(), rawAnthropic.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: err}
		}
	}

	if !streamComplete {
		return rawChat.Bytes(), rawAnthropic.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: io.ErrUnexpectedEOF}
	}

	final, finalizeErr := state.Finalize()
	if finalizeErr != nil {
		return rawChat.Bytes(), rawAnthropic.Bytes(), &streamRelayError{source: streamRelaySourceUpstream, err: finalizeErr}
	}
	rawAnthropic.Write(final)
	if _, writeErr := dst.Write(final); writeErr != nil {
		return rawChat.Bytes(), rawAnthropic.Bytes(), &streamRelayError{source: streamRelaySourceDownstream, err: writeErr}
	}
	dst.(http.Flusher).Flush()
	return rawChat.Bytes(), rawAnthropic.Bytes(), nil
}

func anthropicMessageStopEvent(evt protocol.Event) bool {
	if evt.EventType == "message_stop" {
		return true
	}
	if evt.Data == "" {
		return false
	}
	var payload struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
		return false
	}
	return payload.Type == "message_stop"
}
