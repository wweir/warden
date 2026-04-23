package openai

import (
	"encoding/json"
	"fmt"
)

// EmbeddingsRequest represents a POST /v1/embeddings request.
type EmbeddingsRequest struct {
	Model string                     `json:"model"`
	Input any                        `json:"input"`
	Extra map[string]json.RawMessage `json:"-"`
}

func (r EmbeddingsRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage)
	for k, v := range r.Extra {
		m[k] = v
	}
	if b, err := json.Marshal(r.Model); err == nil {
		m["model"] = b
	}
	if r.Input != nil {
		if b, err := json.Marshal(r.Input); err == nil {
			m["input"] = b
		}
	}
	return json.Marshal(m)
}

func (r *EmbeddingsRequest) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if v, ok := m["model"]; ok {
		_ = json.Unmarshal(v, &r.Model)
		delete(m, "model")
	}
	if v, ok := m["input"]; ok {
		var raw any
		if err := json.Unmarshal(v, &raw); err == nil {
			r.Input = raw
		} else {
			r.Input = v
		}
		delete(m, "input")
	}
	if len(m) > 0 {
		r.Extra = m
	}
	return nil
}

// Validate checks required fields of EmbeddingsRequest.
func (req *EmbeddingsRequest) Validate() error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if req.Input == nil {
		return fmt.Errorf("input is required")
	}
	switch input := req.Input.(type) {
	case string:
		if input == "" {
			return fmt.Errorf("input is required")
		}
	case []any:
		if len(input) == 0 {
			return fmt.Errorf("input is required")
		}
	}
	return nil
}

// EmbeddingData represents a single embedding item in the response.
type EmbeddingData struct {
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
	Index     int       `json:"index"`
}

// EmbeddingsResponse represents the response from POST /v1/embeddings.
type EmbeddingsResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  Usage           `json:"usage,omitempty"`
}
