package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/wweir/warden/pkg/protocol"
	anthproto "github.com/wweir/warden/pkg/protocol/anthropic"
	"github.com/wweir/warden/pkg/protocol/openai"
)

type ErrorSource string

const (
	SourceUpstream   ErrorSource = "upstream"
	SourceDownstream ErrorSource = "downstream"
)

type relayError struct {
	source ErrorSource
	err    error
}

func (e *relayError) Error() string {
	return e.err.Error()
}

func (e *relayError) Unwrap() error {
	return e.err
}

func ErrorSourceOf(err error) ErrorSource {
	var relayErr *relayError
	if errors.As(err, &relayErr) {
		return relayErr.source
	}
	return ""
}

func RelayAnthropicStream(src io.Reader, dst http.ResponseWriter) ([]byte, error) {
	reader := bufio.NewReader(src)
	var raw bytes.Buffer
	streamComplete := false

	for {
		frame, err := ReadSSEFrame(reader)
		if len(frame) > 0 {
			raw.Write(frame)
			events := protocol.ParseEvents(frame)
			for _, evt := range events {
				if anthropicMessageStopEvent(evt) {
					streamComplete = true
				}
			}
			if _, writeErr := dst.Write(frame); writeErr != nil {
				return raw.Bytes(), &relayError{source: SourceDownstream, err: writeErr}
			}
			dst.(http.Flusher).Flush()
		}

		if err != nil {
			if err == io.EOF {
				if !streamComplete {
					return raw.Bytes(), &relayError{source: SourceUpstream, err: io.ErrUnexpectedEOF}
				}
				return raw.Bytes(), nil
			}
			return raw.Bytes(), &relayError{source: SourceUpstream, err: err}
		}
	}
}

func StreamChatAsAnthropic(src io.Reader, dst http.ResponseWriter) ([]byte, []byte, error) {
	reader := bufio.NewReader(src)
	state := anthproto.NewChatToMessagesStreamState()
	var rawChat bytes.Buffer
	var rawAnthropic bytes.Buffer
	streamComplete := false

	for {
		frame, err := ReadSSEFrame(reader)
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
					return rawChat.Bytes(), rawAnthropic.Bytes(), &relayError{source: SourceUpstream, err: convErr}
				}
				if len(converted) == 0 {
					continue
				}
				rawAnthropic.Write(converted)
				if _, writeErr := dst.Write(converted); writeErr != nil {
					return rawChat.Bytes(), rawAnthropic.Bytes(), &relayError{source: SourceDownstream, err: writeErr}
				}
				dst.(http.Flusher).Flush()
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return rawChat.Bytes(), rawAnthropic.Bytes(), &relayError{source: SourceUpstream, err: err}
		}
	}

	if !streamComplete {
		return rawChat.Bytes(), rawAnthropic.Bytes(), &relayError{source: SourceUpstream, err: io.ErrUnexpectedEOF}
	}

	final, finalizeErr := state.Finalize()
	if finalizeErr != nil {
		return rawChat.Bytes(), rawAnthropic.Bytes(), &relayError{source: SourceUpstream, err: finalizeErr}
	}
	rawAnthropic.Write(final)
	if _, writeErr := dst.Write(final); writeErr != nil {
		return rawChat.Bytes(), rawAnthropic.Bytes(), &relayError{source: SourceDownstream, err: writeErr}
	}
	dst.(http.Flusher).Flush()
	return rawChat.Bytes(), rawAnthropic.Bytes(), nil
}

func StreamChatAsResponses(src io.Reader, dst http.ResponseWriter, model string) ([]byte, []byte, error) {
	reader := bufio.NewReader(src)
	state := openai.NewChatResponsesStreamState()
	var rawChat bytes.Buffer
	var rawResp bytes.Buffer
	streamComplete := false

	for {
		frame, err := ReadSSEFrame(reader)
		if len(frame) > 0 {
			rawChat.Write(frame)
			events := protocol.ParseEvents(frame)
			for _, evt := range events {
				if evt.Data == "[DONE]" {
					streamComplete = true
					continue
				}
				converted := state.ConvertEvent(evt)
				if len(converted) == 0 {
					continue
				}
				rawResp.Write(converted)
				if _, writeErr := dst.Write(converted); writeErr != nil {
					return rawChat.Bytes(), rawResp.Bytes(), &relayError{source: SourceDownstream, err: writeErr}
				}
				dst.(http.Flusher).Flush()
			}
		}

		if err != nil {
			if err != io.EOF {
				completed := openai.BuildChatResponsesCompletedEvent(rawChat.Bytes(), model, false)
				rawResp.Write(completed)
				if _, writeErr := dst.Write(completed); writeErr != nil {
					return rawChat.Bytes(), rawResp.Bytes(), &relayError{source: SourceDownstream, err: writeErr}
				}
				dst.(http.Flusher).Flush()
				return rawChat.Bytes(), rawResp.Bytes(), &relayError{source: SourceUpstream, err: err}
			}
			break
		}
	}

	completed := openai.BuildChatResponsesCompletedEvent(rawChat.Bytes(), model, streamComplete)
	rawResp.Write(completed)
	if _, writeErr := dst.Write(completed); writeErr != nil {
		return rawChat.Bytes(), rawResp.Bytes(), &relayError{source: SourceDownstream, err: writeErr}
	}
	dst.(http.Flusher).Flush()
	if !streamComplete {
		return rawChat.Bytes(), rawResp.Bytes(), &relayError{source: SourceUpstream, err: io.ErrUnexpectedEOF}
	}
	return rawChat.Bytes(), rawResp.Bytes(), nil
}

func ReadSSEFrame(r *bufio.Reader) ([]byte, error) {
	var frame bytes.Buffer
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			frame.Write(line)
			if bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")) {
				return frame.Bytes(), nil
			}
		}
		if err != nil {
			if err == io.EOF && frame.Len() > 0 {
				return frame.Bytes(), io.EOF
			}
			return nil, err
		}
	}
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
