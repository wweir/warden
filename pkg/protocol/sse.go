// Package protocol provides LLM protocol-agnostic types and utilities for
// handling streaming responses and tool call extraction across different
// provider protocols (OpenAI, Anthropic, etc.).
package protocol

import (
	"bytes"
	"strings"
)

// Event represents a single Server-Sent Event parsed from a stream.
type Event struct {
	EventType string // event type (empty for default events)
	Data      string // data payload
	Raw       string // original raw text for replay
}

// ParseEvents parses raw SSE bytes into structured events.
// Each event is delimited by a blank line.
func ParseEvents(data []byte) []Event {
	var events []Event
	var currentEvent Event
	var rawLines []string

	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" {
			// blank line = event boundary
			if currentEvent.Data != "" || currentEvent.EventType != "" {
				currentEvent.Raw = strings.Join(rawLines, "\n") + "\n"
				events = append(events, currentEvent)
			}
			currentEvent = Event{}
			rawLines = nil
			continue
		}

		rawLines = append(rawLines, line)

		if after, ok := cutField(line, "event"); ok {
			currentEvent.EventType = after
		} else if after, ok := cutField(line, "data"); ok {
			if currentEvent.Data != "" {
				currentEvent.Data += "\n"
			}
			currentEvent.Data += after
		}
	}

	// handle last event if no trailing blank line
	if currentEvent.Data != "" || currentEvent.EventType != "" {
		currentEvent.Raw = strings.Join(rawLines, "\n") + "\n"
		events = append(events, currentEvent)
	}

	return events
}

// ReplayEvents converts events back to raw bytes for client replay.
func ReplayEvents(events []Event) []byte {
	var buf bytes.Buffer
	for _, e := range events {
		buf.WriteString(e.Raw)
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

// cutField extracts the value of an SSE field from a line.
// Per RFC 8895 the space after the colon is optional: both "field:value" and "field: value" are valid.
func cutField(line, field string) (string, bool) {
	after, ok := strings.CutPrefix(line, field+":")
	if !ok {
		return "", false
	}
	after, _ = strings.CutPrefix(after, " ")
	return after, true
}

// StreamParser extracts tool call information from buffered SSE events.
type StreamParser interface {
	// Parse extracts tool calls from SSE events and reports whether
	// any of them match injected tools.
	Parse(events []Event, injectedTools []string) (toolCalls []ToolCallInfo, hasInjectedToolCall bool, err error)

	// Filter removes injected-tool-related events from the stream for final replay.
	Filter(events []Event, injectedTools []string) []Event
}
