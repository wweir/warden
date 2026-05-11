package proxy

import "encoding/json"

// codexCompatibleModels augments OpenAI-style model entries with the extra
// metadata fields that the Codex CLI catalog expects. Entries that are not
// JSON objects or lack an id/slug are passed through unchanged.
func codexCompatibleModels(models []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(models))
	for _, model := range models {
		entry, ok := modelObject(model)
		if !ok {
			out = append(out, model)
			continue
		}
		id := rawString(entry["id"])
		if id == "" {
			id = rawString(entry["slug"])
		}
		if id == "" {
			out = append(out, model)
			continue
		}

		setStringDefault(entry, "slug", id)
		setStringDefault(entry, "display_name", id)
		setStringDefault(entry, "description", "")
		setStringDefault(entry, "default_reasoning_level", "medium")
		setRawDefault(entry, "supported_reasoning_levels", codexReasoningLevels)
		setStringDefault(entry, "shell_type", "shell_command")
		setStringDefault(entry, "visibility", "list")
		setRawDefault(entry, "supported_in_api", json.RawMessage("true"))
		setRawDefault(entry, "priority", json.RawMessage("0"))
		setRawDefault(entry, "additional_speed_tiers", json.RawMessage("[]"))
		setRawDefault(entry, "availability_nux", json.RawMessage("null"))
		setRawDefault(entry, "upgrade", json.RawMessage("null"))
		setStringDefault(entry, "base_instructions", "")
		setRawDefault(entry, "model_messages", json.RawMessage("{}"))
		setRawDefault(entry, "supports_reasoning_summaries", json.RawMessage("true"))
		setStringDefault(entry, "default_reasoning_summary", "none")
		setRawDefault(entry, "support_verbosity", json.RawMessage("true"))
		setStringDefault(entry, "default_verbosity", "medium")
		setStringDefault(entry, "apply_patch_tool_type", "freeform")
		setStringDefault(entry, "web_search_tool_type", "text")
		setRawDefault(entry, "truncation_policy", json.RawMessage(`{"mode":"tokens","limit":10000}`))
		setRawDefault(entry, "supports_parallel_tool_calls", json.RawMessage("true"))
		setRawDefault(entry, "supports_image_detail_original", json.RawMessage("false"))
		setRawDefault(entry, "context_window", json.RawMessage("128000"))
		setRawDefault(entry, "max_context_window", json.RawMessage("128000"))
		setRawDefault(entry, "effective_context_window_percent", json.RawMessage("95"))
		setRawDefault(entry, "experimental_supported_tools", json.RawMessage("[]"))
		setRawDefault(entry, "input_modalities", json.RawMessage(`["text"]`))
		setRawDefault(entry, "supports_search_tool", json.RawMessage("true"))

		encoded, err := json.Marshal(entry)
		if err != nil {
			out = append(out, model)
			continue
		}
		out = append(out, encoded)
	}
	return out
}

var codexReasoningLevels = json.RawMessage(`[
	{"effort":"low","description":"Fast responses with lighter reasoning"},
	{"effort":"medium","description":"Balances speed and reasoning depth for everyday tasks"},
	{"effort":"high","description":"Greater reasoning depth for complex problems"},
	{"effort":"xhigh","description":"Extra high reasoning depth for complex problems"}
]`)

func modelObject(model json.RawMessage) (map[string]json.RawMessage, bool) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(model, &entry); err != nil {
		return nil, false
	}
	return entry, true
}

func rawString(raw json.RawMessage) string {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func setStringDefault(entry map[string]json.RawMessage, key, value string) {
	if len(entry[key]) > 0 {
		return
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	entry[key] = encoded
}

func setRawDefault(entry map[string]json.RawMessage, key string, value json.RawMessage) {
	if len(entry[key]) > 0 {
		return
	}
	entry[key] = value
}
