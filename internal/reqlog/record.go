package reqlog

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// GenerateID returns an 8-character hex string for request identification.
func GenerateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Sanitize ensures all json.RawMessage fields contain valid JSON.
func (r *Record) Sanitize() {
	r.Request = ensureJSON(r.Request)
	r.Response = ensureJSON(r.Response)
	for i := range r.Steps {
		r.Steps[i].LLMRequest = ensureJSON(r.Steps[i].LLMRequest)
		r.Steps[i].LLMResponse = ensureJSON(r.Steps[i].LLMResponse)
	}
}

// ensureJSON returns raw as-is if it is valid JSON, otherwise wraps it as a JSON string.
func ensureJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || json.Valid(raw) {
		return raw
	}
	b, _ := json.Marshal(string(raw))
	return b
}
