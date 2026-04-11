package tokenusage

import (
	"encoding/json"

	"github.com/tidwall/gjson"
	"github.com/wweir/warden/config"
	"github.com/wweir/warden/pkg/protocol"
)

const (
	CompletenessExact   = "exact"
	CompletenessPartial = "partial"
	CompletenessMissing = "missing"

	SourceReportedJSON     = "reported_json"
	SourceReportedSSE      = "reported_sse"
	SourceBridgeNormalized = "bridge_normalized"
)

type Observation struct {
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens,omitempty"`
	Source           string `json:"source,omitempty"`
	Completeness     string `json:"completeness,omitempty"`

	promptObserved     bool
	completionObserved bool
}

func (o Observation) IsExact() bool {
	return o.CompletenessLabel() == CompletenessExact
}

func (o Observation) HasUsage() bool {
	return o.promptObserved || o.completionObserved
}

func (o Observation) SourceLabel() string {
	if o.Source == "" {
		return "unknown"
	}
	return o.Source
}

func (o Observation) CompletenessLabel() string {
	if o.Completeness != "" {
		return o.Completeness
	}
	switch {
	case o.promptObserved && o.completionObserved:
		return CompletenessExact
	case o.promptObserved || o.completionObserved:
		return CompletenessPartial
	default:
		return CompletenessMissing
	}
}

func (o Observation) WithSource(source string) Observation {
	if source != "" {
		o.Source = source
	}
	if o.Completeness == "" {
		o.Completeness = o.CompletenessLabel()
	}
	return o
}

func Missing(source string) Observation {
	return Observation{
		Source:       source,
		Completeness: CompletenessMissing,
	}
}

func FromJSON(body []byte) Observation {
	return fromJSONBody(body, SourceReportedJSON)
}

func FromStream(serviceProtocol, providerProtocol string, body []byte) Observation {
	switch {
	case providerProtocol == config.ProviderProtocolAnthropic:
		return FromAnthropicStream(body)
	case config.IsResponsesRouteProtocol(serviceProtocol):
		return FromResponsesStream(body)
	default:
		return FromOpenAIChatStream(body)
	}
}

func FromOpenAIChatStream(body []byte) Observation {
	best := Missing(SourceReportedSSE)
	for _, evt := range protocol.ParseEvents(body) {
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}
		if obs, ok := observationFromUsageResult(gjson.Get(evt.Data, "usage"), SourceReportedSSE); ok {
			best = obs
		}
	}
	return best
}

func FromResponsesStream(body []byte) Observation {
	best := Missing(SourceReportedSSE)
	for _, evt := range protocol.ParseEvents(body) {
		if evt.Data == "" || evt.Data == "[DONE]" {
			continue
		}
		if obs, ok := observationFromUsageResult(gjson.Get(evt.Data, "response.usage"), SourceReportedSSE); ok {
			best = obs
			continue
		}
		if obs, ok := observationFromUsageResult(gjson.Get(evt.Data, "usage"), SourceReportedSSE); ok {
			best = obs
		}
	}
	return best
}

func FromAnthropicStream(body []byte) Observation {
	if len(body) == 0 {
		return Missing(SourceReportedSSE)
	}

	var (
		promptObserved     bool
		promptTokens       int64
		completionObserved bool
		completionTokens   int64
		totalObserved      bool
		totalTokens        int64
	)

	for _, evt := range protocol.ParseEvents(body) {
		if evt.Data == "" {
			continue
		}

		var payload struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(evt.Data), &payload); err != nil {
			continue
		}

		switch payload.Type {
		case "message_start":
			usage := gjson.Get(evt.Data, "message.usage")
			input := usage.Get("input_tokens")
			if input.Exists() {
				promptObserved = true
				promptTokens = input.Int()
			}
			total := usage.Get("total_tokens")
			if total.Exists() {
				totalObserved = true
				totalTokens = total.Int()
			}
		case "message_delta":
			usage := gjson.Get(evt.Data, "usage")
			if usage.Exists() && usage.IsObject() {
				output := usage.Get("output_tokens")
				if output.Exists() {
					completionObserved = true
					completionTokens = output.Int()
				}
				total := usage.Get("total_tokens")
				if total.Exists() {
					totalObserved = true
					totalTokens = total.Int()
				}
			}
		}
	}

	return buildObservation(promptObserved, promptTokens, completionObserved, completionTokens, totalObserved, totalTokens, SourceReportedSSE)
}

func fromJSONBody(body []byte, source string) Observation {
	if len(body) == 0 {
		return Missing(source)
	}

	if obs, ok := observationFromUsageResult(gjson.GetBytes(body, "usage"), source); ok {
		return obs
	}
	if obs, ok := observationFromUsageResult(gjson.GetBytes(body, "response.usage"), source); ok {
		return obs
	}
	return Missing(source)
}

func observationFromUsageResult(usage gjson.Result, source string) (Observation, bool) {
	if !usage.Exists() || !usage.IsObject() {
		return Observation{}, false
	}

	prompt := usage.Get("prompt_tokens")
	completion := usage.Get("completion_tokens")
	if !prompt.Exists() && !completion.Exists() {
		prompt = usage.Get("input_tokens")
		completion = usage.Get("output_tokens")
	}
	total := usage.Get("total_tokens")

	if !prompt.Exists() && !completion.Exists() && !total.Exists() {
		return Observation{}, false
	}

	return buildObservation(
		prompt.Exists(),
		prompt.Int(),
		completion.Exists(),
		completion.Int(),
		total.Exists(),
		total.Int(),
		source,
	), true
}

func buildObservation(promptObserved bool, promptTokens int64, completionObserved bool, completionTokens int64, totalObserved bool, totalTokens int64, source string) Observation {
	obs := Observation{
		PromptTokens:       promptTokens,
		CompletionTokens:   completionTokens,
		Source:             source,
		promptObserved:     promptObserved,
		completionObserved: completionObserved,
	}

	if totalObserved {
		obs.TotalTokens = totalTokens
	} else if promptObserved && completionObserved {
		obs.TotalTokens = promptTokens + completionTokens
	}
	obs.Completeness = obs.CompletenessLabel()
	return obs
}
