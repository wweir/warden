package openai

// DowngradeDeveloperMessages converts developer-role messages into system-role messages.
// Some OpenAI-compatible providers reject developer even though the rest of the chat schema matches.
func DowngradeDeveloperMessages(messages []Message) ([]Message, bool) {
	if len(messages) == 0 {
		return nil, false
	}

	cloned := make([]Message, len(messages))
	changed := false
	for i, msg := range messages {
		cloned[i] = msg
		if msg.Role == "developer" {
			cloned[i].Role = "system"
			changed = true
		}
	}

	if !changed {
		return messages, false
	}
	return cloned, true
}
