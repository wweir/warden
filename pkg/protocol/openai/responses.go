package openai

import (
	"encoding/json"
	"fmt"
	"maps"
)

// ResponsesRequest represents a POST /v1/responses request.
// Only fields the gateway needs to inspect/modify are strongly typed;
// everything else is preserved via Extra.
type ResponsesRequest struct {
	Model  string                     `json:"model"`
	Input  json.RawMessage            `json:"input"`           // string or []Item, parsed lazily
	Tools  []json.RawMessage          `json:"tools,omitempty"` // keep RawMessage to passthrough non-function tools
	Stream bool                       `json:"stream,omitempty"`
	Extra  map[string]json.RawMessage `json:"-"`
}

func (r ResponsesRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	maps.Copy(m, r.Extra)
	if b, err := json.Marshal(r.Model); err == nil {
		m["model"] = b
	}
	m["input"] = r.Input
	if len(r.Tools) > 0 {
		if b, err := json.Marshal(r.Tools); err == nil {
			m["tools"] = b
		}
	}
	if r.Stream {
		m["stream"] = json.RawMessage(`true`)
	}
	return json.Marshal(m)
}

func (r *ResponsesRequest) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["model"]; ok {
		json.Unmarshal(v, &r.Model)
		delete(m, "model")
	}
	if v, ok := m["input"]; ok {
		r.Input = v
		delete(m, "input")
	}
	if v, ok := m["tools"]; ok {
		json.Unmarshal(v, &r.Tools)
		delete(m, "tools")
	}
	if v, ok := m["stream"]; ok {
		json.Unmarshal(v, &r.Stream)
		delete(m, "stream")
	}
	if len(m) > 0 {
		r.Extra = m
	}
	return nil
}

// Validate checks required fields of ResponsesRequest.
func (req *ResponsesRequest) Validate() error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Input) == 0 {
		return fmt.Errorf("input is required")
	}
	return nil
}

// ResponsesResponse represents the response from POST /v1/responses.
type ResponsesResponse struct {
	ID     string                     `json:"id"`
	Status string                     `json:"status,omitempty"` // "completed", "incomplete", "failed"
	Output []json.RawMessage          `json:"output"` // output items, keep as RawMessage
	Extra  map[string]json.RawMessage `json:"-"`
}

func (r ResponsesResponse) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	maps.Copy(m, r.Extra)
	if b, err := json.Marshal(r.ID); err == nil {
		m["id"] = b
	}
	if r.Status != "" {
		if b, err := json.Marshal(r.Status); err == nil {
			m["status"] = b
		}
	}
	if b, err := json.Marshal(r.Output); err == nil {
		m["output"] = b
	}
	return json.Marshal(m)
}

func (r *ResponsesResponse) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["id"]; ok {
		json.Unmarshal(v, &r.ID)
		delete(m, "id")
	}
	if v, ok := m["status"]; ok {
		json.Unmarshal(v, &r.Status)
		delete(m, "status")
	}
	if v, ok := m["output"]; ok {
		json.Unmarshal(v, &r.Output)
		delete(m, "output")
	}
	if len(m) > 0 {
		r.Extra = m
	}
	return nil
}

// FunctionCallItem represents a function_call output item.
// Parsed from output[] when type == "function_call".
type FunctionCallItem struct {
	Type      string                     `json:"type"` // always "function_call"
	CallID    string                     `json:"call_id"`
	Name      string                     `json:"name"`
	Arguments string                     `json:"arguments"`
	ID        string                     `json:"id,omitempty"`
	Status    string                     `json:"status,omitempty"`
	Extra     map[string]json.RawMessage `json:"-"`
}

func (f FunctionCallItem) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	maps.Copy(m, f.Extra)
	if b, err := json.Marshal(f.Type); err == nil {
		m["type"] = b
	}
	if b, err := json.Marshal(f.CallID); err == nil {
		m["call_id"] = b
	}
	if b, err := json.Marshal(f.Name); err == nil {
		m["name"] = b
	}
	if b, err := json.Marshal(f.Arguments); err == nil {
		m["arguments"] = b
	}
	if f.ID != "" {
		if b, err := json.Marshal(f.ID); err == nil {
			m["id"] = b
		}
	}
	if f.Status != "" {
		if b, err := json.Marshal(f.Status); err == nil {
			m["status"] = b
		}
	}
	return json.Marshal(m)
}

func (f *FunctionCallItem) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["type"]; ok {
		json.Unmarshal(v, &f.Type)
		delete(m, "type")
	}
	if v, ok := m["call_id"]; ok {
		json.Unmarshal(v, &f.CallID)
		delete(m, "call_id")
	}
	if v, ok := m["name"]; ok {
		json.Unmarshal(v, &f.Name)
		delete(m, "name")
	}
	if v, ok := m["arguments"]; ok {
		json.Unmarshal(v, &f.Arguments)
		delete(m, "arguments")
	}
	if v, ok := m["id"]; ok {
		json.Unmarshal(v, &f.ID)
		delete(m, "id")
	}
	if v, ok := m["status"]; ok {
		json.Unmarshal(v, &f.Status)
		delete(m, "status")
	}
	if len(m) > 0 {
		f.Extra = m
	}
	return nil
}

// FunctionCallOutputItem represents a function_call_output input item
// constructed by the gateway after executing injected tools.
type FunctionCallOutputItem struct {
	Type   string `json:"type"` // always "function_call_output"
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// ResponsesFunctionTool represents a function tool definition
// in the Responses API flat format.
type ResponsesFunctionTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}
