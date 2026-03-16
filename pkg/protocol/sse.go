// Package protocol provides LLM protocol-agnostic types and utilities for
// handling streaming responses and tool call extraction across different
// provider protocols (OpenAI, Anthropic, etc.).
package protocol

import (
	"bytes"
	"strconv"
	"strings"
)

// Event represents a single Server-Sent Event parsed from a stream.
type Event struct {
	EventType string // event type (empty for default events)
	Data      string // data payload
	HasData   bool   // whether the frame explicitly contained at least one data field
	ID        string // event ID from the id field
	HasID     bool   // whether the frame explicitly contained an id field
	Retry     *int   // reconnection delay from the retry field, if valid
	Comments  []string
	Raw       string // original raw text for replay
}

// ParseEvents parses raw SSE bytes into structured events.
// Each event is delimited by a blank line.
func ParseEvents(data []byte) []Event {
	var events []Event
	var currentEvent Event
	var rawLines []string

	flushEvent := func() {
		if len(rawLines) == 0 {
			return
		}
		currentEvent.Raw = strings.Join(rawLines, "\n") + "\n"
		events = append(events, currentEvent)
		currentEvent = Event{}
		rawLines = nil
	}

	forEachSSELine(data, func(line string) {
		if line == "" {
			// blank line = event boundary
			flushEvent()
			return
		}

		rawLines = append(rawLines, line)

		if strings.HasPrefix(line, ":") {
			currentEvent.Comments = append(currentEvent.Comments, line[1:])
			return
		}

		field, value := splitField(line)
		switch field {
		case "event":
			currentEvent.EventType = value
		case "data":
			currentEvent.HasData = true
			if currentEvent.Data != "" {
				currentEvent.Data += "\n"
			}
			currentEvent.Data += value
		case "id":
			if strings.ContainsRune(value, '\x00') {
				return
			}
			currentEvent.ID = value
			currentEvent.HasID = true
		case "retry":
			if retry, ok := parseRetry(value); ok {
				currentEvent.Retry = &retry
			}
		}
	})

	// handle last event if no trailing blank line
	flushEvent()

	return events
}

// ReplayEvents converts events back to raw bytes for client replay.
func ReplayEvents(events []Event) []byte {
	var buf bytes.Buffer
	for _, e := range events {
		if e.Raw != "" {
			buf.WriteString(e.Raw)
		} else {
			writeEvent(&buf, e)
		}
		buf.WriteByte('\n') // blank line between events
	}
	return buf.Bytes()
}

// ToolCallInfo is the protocol-agnostic representation of a tool call
// extracted from SSE stream events.
type ToolCallInfo struct {
	ID        string // tool call ID
	Name      string // function name
	Arguments string // function arguments JSON
}

// GetName returns the function name for the tool call (implements namedItem constraint).
func (tci ToolCallInfo) GetName() string {
	return tci.Name
}

func forEachSSELine(data []byte, fn func(string)) {
	start := 0
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			line := string(data[start:i])
			if strings.HasSuffix(line, "\r") {
				line = strings.TrimSuffix(line, "\r")
			}
			fn(line)
			start = i + 1
		case '\r':
			fn(string(data[start:i]))
			if i+1 < len(data) && data[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(data) {
		fn(string(data[start:]))
	}
}

// splitField extracts the field name and value from one SSE line.
// Per the HTML Living Standard, the space after ":" is optional.
func splitField(line string) (field, value string) {
	field, value, found := strings.Cut(line, ":")
	if !found {
		return line, ""
	}
	value, _ = strings.CutPrefix(value, " ")
	return field, value
}

func parseRetry(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	retry, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}
	return retry, true
}

func writeEvent(buf *bytes.Buffer, evt Event) {
	for _, comment := range evt.Comments {
		buf.WriteByte(':')
		buf.WriteString(comment)
		buf.WriteByte('\n')
	}
	if evt.EventType != "" {
		writeField(buf, "event", evt.EventType)
	}
	if evt.HasID || evt.ID != "" {
		writeField(buf, "id", evt.ID)
	}
	if evt.Retry != nil {
		writeField(buf, "retry", strconv.Itoa(*evt.Retry))
	}
	if evt.HasData || evt.Data != "" {
		for _, line := range strings.Split(evt.Data, "\n") {
			writeField(buf, "data", line)
		}
	}
}

func writeField(buf *bytes.Buffer, field, value string) {
	buf.WriteString(field)
	buf.WriteByte(':')
	if value != "" {
		buf.WriteByte(' ')
		buf.WriteString(value)
	}
	buf.WriteByte('\n')
}

// StreamParser extracts tool call information from buffered SSE events.
type StreamParser interface {
	Parse(events []Event) (toolCalls []ToolCallInfo, err error)
}
