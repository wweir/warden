package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/pkg/protocol"
)

// ChatStreamParser parses SSE events from OpenAI Chat Completions streaming responses.
type ChatStreamParser struct{}

func (p *ChatStreamParser) Parse(events []protocol.Event) ([]protocol.ToolCallInfo, error) {
	type deltaToolCall struct {
		Index    int    `json:"index"`
		ID       string `json:"id,omitempty"`
		Type     string `json:"type,omitempty"`
		Function struct {
			Name      string `json:"name,omitempty"`
			Arguments string `json:"arguments,omitempty"`
		} `json:"function"`
	}
	type chunk struct {
		Choices []struct {
			Delta struct {
				ToolCalls []deltaToolCall `json:"tool_calls,omitempty"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason,omitempty"`
		} `json:"choices"`
	}

	toolCallMap := make(map[int]*ToolCall)
	var finishReason string

	for _, evt := range events {
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}

		var c chunk
		if err := json.Unmarshal([]byte(evt.Data), &c); err != nil {
			continue
		}
		if len(c.Choices) == 0 {
			continue
		}

		if c.Choices[0].FinishReason != "" {
			finishReason = c.Choices[0].FinishReason
		}

		for _, dtc := range c.Choices[0].Delta.ToolCalls {
			existing, ok := toolCallMap[dtc.Index]
			if !ok {
				toolCallMap[dtc.Index] = &ToolCall{
					ID:   dtc.ID,
					Type: dtc.Type,
					Function: FunctionCall{
						Name:      dtc.Function.Name,
						Arguments: dtc.Function.Arguments,
					},
				}
			} else {
				existing.Function.Arguments += dtc.Function.Arguments
			}
		}
	}

	if finishReason != "tool_calls" || len(toolCallMap) == 0 {
		return nil, nil
	}

	var infos []protocol.ToolCallInfo
	for i := range len(toolCallMap) {
		tc, ok := toolCallMap[i]
		if !ok {
			continue
		}
		infos = append(infos, protocol.ToolCallInfo{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	return infos, nil
}

// ResponsesStreamParser parses SSE events from OpenAI Responses API streaming responses.
type ResponsesStreamParser struct{}

func (p *ResponsesStreamParser) Parse(events []protocol.Event) ([]protocol.ToolCallInfo, error) {
	acc := newResponsesToolAccumulator()

	for _, evt := range events {
		switch responsesEventType(evt) {
		case "response.completed":
			infos, err := parseCompletedResponsesToolCalls(evt.Data)
			if err != nil {
				return nil, err
			}
			if len(infos) > 0 {
				return infos, nil
			}
		case "response.output_item.added", "response.output_item.done":
			acc.observeItem(gjson.Parse(evt.Data))
		case "response.function_call_arguments.delta":
			acc.appendArgumentsDelta(gjson.Parse(evt.Data))
		case "response.function_call_arguments.done":
			acc.setArguments(gjson.Parse(evt.Data))
		}
	}

	return acc.infosOrNil(), nil
}

func parseCompletedResponsesToolCalls(data string) ([]protocol.ToolCallInfo, error) {
	var wrapper struct {
		Response ResponsesResponse `json:"response"`
	}
	if err := json.Unmarshal([]byte(data), &wrapper); err != nil {
		return nil, err
	}
	return functionCallInfosFromResponseOutput(wrapper.Response.Output), nil
}

func functionCallInfosFromResponseOutput(output []json.RawMessage) []protocol.ToolCallInfo {
	var infos []protocol.ToolCallInfo
	for _, raw := range output {
		var typeCheck struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &typeCheck); err != nil || typeCheck.Type != "function_call" {
			continue
		}

		var fc FunctionCallItem
		if err := json.Unmarshal(raw, &fc); err != nil {
			continue
		}

		infos = append(infos, protocol.ToolCallInfo{
			ID:        firstNonEmptyString(fc.CallID, fc.ID),
			Name:      fc.Name,
			Arguments: fc.Arguments,
		})
	}
	return infos
}

type responsesToolAccumulator struct {
	infos         []protocol.ToolCallInfo
	byCallID      map[string]int
	byItemID      map[string]int
	byOutputIndex map[int]int
	lastIndex     int
}

func newResponsesToolAccumulator() *responsesToolAccumulator {
	return &responsesToolAccumulator{
		byCallID:      make(map[string]int),
		byItemID:      make(map[string]int),
		byOutputIndex: make(map[int]int),
		lastIndex:     -1,
	}
}

func (a *responsesToolAccumulator) infosOrNil() []protocol.ToolCallInfo {
	if len(a.infos) == 0 {
		return nil
	}
	return a.infos
}

func (a *responsesToolAccumulator) observeItem(result gjson.Result) {
	item := result.Get("item")
	if !item.Exists() {
		item = result
	}
	if item.Get("type").String() != "function_call" {
		return
	}

	outputIndex, hasOutputIndex := responsesOutputIndex(item)
	idx := a.ensureIndex(item.Get("call_id").String(), item.Get("id").String(), outputIndex, hasOutputIndex)
	info := &a.infos[idx]
	if info.ID == "" {
		info.ID = firstNonEmptyString(item.Get("call_id").String(), item.Get("id").String())
	}
	if info.Name == "" {
		info.Name = item.Get("name").String()
	}
	if args := resultStringOrRaw(item.Get("arguments")); args != "" && info.Arguments == "" {
		info.Arguments = args
	}
	a.lastIndex = idx
}

func (a *responsesToolAccumulator) appendArgumentsDelta(result gjson.Result) {
	delta := result.Get("delta").String()
	if delta == "" {
		return
	}
	idx := a.lookupIndex(result)
	if idx < 0 {
		return
	}
	a.infos[idx].Arguments += delta
	a.lastIndex = idx
}

func (a *responsesToolAccumulator) setArguments(result gjson.Result) {
	args := resultStringOrRaw(result.Get("arguments"))
	if args == "" {
		return
	}
	idx := a.lookupIndex(result)
	if idx < 0 {
		return
	}
	a.infos[idx].Arguments = args
	a.lastIndex = idx
}

func (a *responsesToolAccumulator) ensureIndex(callID, itemID string, outputIndex int, hasOutputIndex bool) int {
	if idx := a.lookupByKey(callID, itemID, outputIndex, hasOutputIndex); idx >= 0 {
		a.recordKeys(idx, callID, itemID, outputIndex, hasOutputIndex)
		return idx
	}

	idx := len(a.infos)
	a.infos = append(a.infos, protocol.ToolCallInfo{})
	a.recordKeys(idx, callID, itemID, outputIndex, hasOutputIndex)
	return idx
}

func (a *responsesToolAccumulator) lookupIndex(result gjson.Result) int {
	callID := result.Get("call_id").String()
	itemID := firstNonEmptyString(result.Get("item_id").String(), result.Get("id").String())
	outputIndex, hasOutputIndex := responsesOutputIndex(result)
	if idx := a.lookupByKey(callID, itemID, outputIndex, hasOutputIndex); idx >= 0 {
		return idx
	}
	if a.lastIndex >= 0 {
		return a.lastIndex
	}
	if len(a.infos) == 1 {
		return 0
	}
	return -1
}

func (a *responsesToolAccumulator) lookupByKey(callID, itemID string, outputIndex int, hasOutputIndex bool) int {
	if callID != "" {
		if idx, ok := a.byCallID[callID]; ok {
			return idx
		}
	}
	if itemID != "" {
		if idx, ok := a.byItemID[itemID]; ok {
			return idx
		}
	}
	if hasOutputIndex {
		if idx, ok := a.byOutputIndex[outputIndex]; ok {
			return idx
		}
	}
	return -1
}

func (a *responsesToolAccumulator) recordKeys(idx int, callID, itemID string, outputIndex int, hasOutputIndex bool) {
	if callID != "" {
		a.byCallID[callID] = idx
	}
	if itemID != "" {
		a.byItemID[itemID] = idx
	}
	if hasOutputIndex {
		a.byOutputIndex[outputIndex] = idx
	}
}

func responsesOutputIndex(result gjson.Result) (int, bool) {
	for _, key := range []string{"output_index", "item_index", "index"} {
		value := result.Get(key)
		if value.Exists() && value.Type == gjson.Number {
			return int(value.Int()), true
		}
	}
	return 0, false
}

func resultStringOrRaw(result gjson.Result) string {
	if !result.Exists() {
		return ""
	}
	if result.Type == gjson.String {
		return result.String()
	}
	return result.Raw
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// AssembleResponsesStream extracts the completed Responses API object from an SSE stream
// so streaming logs keep the same shape as non-streaming responses.
func AssembleResponsesStream(rawSSE []byte) ([]byte, error) {
	events := protocol.ParseEvents(rawSSE)
	resp := ExtractCompletedResponse(events)
	if resp == nil {
		return nil, fmt.Errorf("response.completed event not found")
	}
	return json.Marshal(resp)
}

// AssembleChatStream merges streaming SSE chunks into a single JSON object
// for logging. It uses map[string]any to avoid coupling to any specific API
// schema, maximizing compatibility with non-standard LLM providers.
//
// Strategy: take the last chunk as the base object (it typically carries
// top-level fields like id, model, usage, etc.), then merge accumulated
// delta content and tool_calls into choices[0].message.
func AssembleChatStream(rawSSE []byte) ([]byte, error) {
	events := protocol.ParseEvents(rawSSE)

	var (
		base         map[string]any                 // last parsed chunk as base
		contentParts []string                       // accumulated delta.content
		toolCalls    = make(map[int]map[string]any) // index -> merged tool_call
		role         string
		finishReason string
	)

	for _, evt := range events {
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}

		var chunk map[string]any
		if err := json.Unmarshal([]byte(evt.Data), &chunk); err != nil {
			continue
		}
		base = chunk

		choices, _ := asArray(chunk["choices"])
		if len(choices) == 0 {
			continue
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			continue
		}

		if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
			finishReason = fr
		}

		delta, _ := choice["delta"].(map[string]any)
		if delta == nil {
			continue
		}

		if r, ok := delta["role"].(string); ok && r != "" {
			role = r
		}
		if c, ok := delta["content"].(string); ok && c != "" {
			contentParts = append(contentParts, c)
		}

		// merge streamed tool_calls by index
		deltaTCs, _ := asArray(delta["tool_calls"])
		for _, raw := range deltaTCs {
			dtc, _ := raw.(map[string]any)
			if dtc == nil {
				continue
			}
			idx := int(toFloat64(dtc["index"]))
			existing, ok := toolCalls[idx]
			if !ok {
				// first chunk for this index: clone the object
				existing = make(map[string]any)
				for k, v := range dtc {
					if k != "index" {
						existing[k] = v
					}
				}
				toolCalls[idx] = existing
				continue
			}
			// subsequent chunks: append function.arguments
			fn, _ := dtc["function"].(map[string]any)
			if fn == nil {
				continue
			}
			existFn, _ := existing["function"].(map[string]any)
			if existFn == nil {
				continue
			}
			if args, ok := fn["arguments"].(string); ok {
				prev, _ := existFn["arguments"].(string)
				existFn["arguments"] = prev + args
			}
		}
	}

	if base == nil {
		return rawSSE, nil
	}

	// build the assembled message
	msg := make(map[string]any)
	if role != "" {
		msg["role"] = role
	}
	if len(contentParts) > 0 {
		msg["content"] = strings.Join(contentParts, "")
	}
	if len(toolCalls) > 0 {
		sorted := make([]any, 0, len(toolCalls))
		for i := range len(toolCalls) {
			if tc, ok := toolCalls[i]; ok {
				sorted = append(sorted, tc)
			}
		}
		msg["tool_calls"] = sorted
	}

	// build the single choice with "message" instead of "delta"
	choice := map[string]any{
		"index":   0,
		"message": msg,
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}

	// reuse base for top-level fields, replace choices
	base["choices"] = []any{choice}
	// remove streaming-specific object type
	if obj, ok := base["object"].(string); ok && obj == "chat.completion.chunk" {
		base["object"] = "chat.completion"
	}

	return json.Marshal(base)
}

func asArray(v any) ([]any, bool) {
	arr, ok := v.([]any)
	return arr, ok
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

// ExtractCompletedResponse finds the response.completed event and returns the response.
func ExtractCompletedResponse(events []protocol.Event) *ResponsesResponse {
	for _, evt := range events {
		if responsesEventType(evt) != "response.completed" {
			continue
		}
		if resp := completedResponseFromData(evt.Data); resp != nil {
			return resp
		}
	}
	return nil
}

func responsesEventType(evt protocol.Event) string {
	if evt.EventType != "" {
		return evt.EventType
	}

	var typeCheck struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(evt.Data), &typeCheck); err != nil {
		return ""
	}
	return typeCheck.Type
}

func completedResponseFromData(data string) *ResponsesResponse {
	var wrapper struct {
		Response *ResponsesResponse `json:"response"`
	}
	if err := json.Unmarshal([]byte(data), &wrapper); err == nil && wrapper.Response != nil {
		return wrapper.Response
	}

	var resp ResponsesResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return nil
	}
	if resp.ID == "" && resp.Status == "" && resp.Output == nil && len(resp.Extra) == 0 {
		return nil
	}
	return &resp
}
