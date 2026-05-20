package tokenusage

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tiktoken-go/tokenizer"
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
	SourceEstimated        = "estimated"
)

type Observation struct {
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	CacheTokens      int64  `json:"cache_tokens,omitempty"`
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

func FromEmbeddingsJSON(body []byte) Observation {
	obs := fromJSONBody(body, SourceReportedJSON)
	if !obs.promptObserved || obs.completionObserved {
		return obs
	}
	if obs.TotalTokens > 0 && obs.TotalTokens == obs.PromptTokens {
		obs.CompletionTokens = 0
		obs.completionObserved = true
		obs.Completeness = CompletenessExact
	}
	return obs
}

func FromStream(serviceProtocol, providerProtocol string, body []byte) Observation {
	switch {
	case providerProtocol == config.ProviderFormatAnthropic:
		return FromAnthropicStream(body)
	case serviceProtocol == config.RouteProtocolResponses:
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
		cacheTokens        int64
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
			cacheTokens = cacheTokensFromUsage(usage)
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

	obs := buildObservation(promptObserved, promptTokens, completionObserved, completionTokens, totalObserved, totalTokens, SourceReportedSSE)
	obs.CacheTokens = cacheTokens
	return obs
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
	cache := cacheTokensFromUsage(usage)

	if !prompt.Exists() && !completion.Exists() && !total.Exists() {
		return Observation{}, false
	}

	obs := buildObservation(
		prompt.Exists(),
		prompt.Int(),
		completion.Exists(),
		completion.Int(),
		total.Exists(),
		total.Int(),
		source,
	)
	obs.CacheTokens = cache
	return obs, true
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

func cacheTokensFromUsage(usage gjson.Result) int64 {
	if !usage.Exists() || !usage.IsObject() {
		return 0
	}

	if cached := usage.Get("prompt_tokens_details.cached_tokens"); cached.Exists() {
		return cached.Int()
	}
	if cached := usage.Get("input_tokens_details.cached_tokens"); cached.Exists() {
		return cached.Int()
	}

	return usage.Get("cache_creation_input_tokens").Int() + usage.Get("cache_read_input_tokens").Int()
}

// codecCache holds reusable tokenizer instances to avoid repeated regexp compilation.
// tokenizer.Get creates a new Codec each time; the underlying vocab is cached via sync.Once,
// but the regexp is recompiled on every call. Caching at the application layer eliminates that overhead.
var codecCache = struct {
	once  sync.Once
	o200k tokenizer.Codec
	cl100 tokenizer.Codec
	err   error
}{}

func initCodecCache() {
	codecCache.o200k, codecCache.err = tokenizer.Get(tokenizer.O200kBase)
	if codecCache.err != nil {
		return
	}
	codecCache.cl100, codecCache.err = tokenizer.Get(tokenizer.Cl100kBase)
}

// EstimateTokenUsage estimates token usage from request/response when upstream doesn't provide it.
// It uses tiktoken to count tokens in the input messages and accumulated output.
// The tokenizer is selected based on the model name.
func EstimateTokenUsage(reqBody []byte, model string, accumulatedOutput string) Observation {
	codecCache.once.Do(initCodecCache)
	if codecCache.err != nil {
		return Missing(SourceEstimated)
	}

	codec := codecForModelWithLookup(model, tokenizer.ForModel)
	if codec == nil {
		return Missing(SourceEstimated)
	}

	var promptTokens, completionTokens int64

	for _, text := range ExtractInputText(reqBody) {
		if tokens, err := codec.Count(text); err == nil {
			promptTokens += int64(tokens)
		}
	}

	// Estimate completion tokens from accumulated output
	if len(accumulatedOutput) > 0 {
		if tokens, err := codec.Count(accumulatedOutput); err == nil {
			completionTokens = int64(tokens)
		}
	}

	if promptTokens == 0 && completionTokens == 0 {
		return Missing(SourceEstimated)
	}

	return Observation{
		PromptTokens:       promptTokens,
		CompletionTokens:   completionTokens,
		TotalTokens:        promptTokens + completionTokens,
		Source:             SourceEstimated,
		Completeness:       CompletenessPartial,
		promptObserved:     promptTokens > 0,
		completionObserved: completionTokens > 0,
	}
}

func codecForModelWithLookup(model string, lookup func(tokenizer.Model) (tokenizer.Codec, error)) tokenizer.Codec {
	lower := strings.ToLower(strings.TrimSpace(model))
	if lower == "" {
		return codecCache.cl100
	}

	if lookup != nil {
		if codec, err := lookup(tokenizer.Model(lower)); err == nil && codec != nil {
			return codec
		}
	}

	// Prefer cached codecs for known model families. The tokenizer package supports
	// exact model lookup, but newer model suffixes are not always in sync here.
	switch {
	case strings.HasPrefix(lower, "o1"),
		strings.HasPrefix(lower, "o3"),
		strings.HasPrefix(lower, "o4"),
		strings.HasPrefix(lower, "gpt-4o"),
		strings.HasPrefix(lower, "gpt-4.1"),
		strings.HasPrefix(lower, "gpt-4.5"),
		strings.HasPrefix(lower, "gpt-5"),
		strings.HasPrefix(lower, "chatgpt-4o"):
		return codecCache.o200k
	default:
		return codecCache.cl100
	}
}

func codecForModel(model string) tokenizer.Codec {
	return codecForModelWithLookup(model, tokenizer.ForModel)
}

func ExtractInputText(reqBody []byte) []string {
	if len(reqBody) == 0 {
		return nil
	}

	var body map[string]any
	if err := json.Unmarshal(reqBody, &body); err != nil {
		return nil
	}

	if messages, ok := body["messages"].([]any); ok {
		return extractMessagesText(messages)
	}

	texts := extractResponsesInputText(body["input"])
	if instructions, ok := body["instructions"].(string); ok && strings.TrimSpace(instructions) != "" {
		texts = append([]string{instructions}, texts...)
	}
	return texts
}

func extractMessagesText(messages []any) []string {
	var texts []string
	for _, rawMsg := range messages {
		msg, ok := rawMsg.(map[string]any)
		if !ok {
			continue
		}
		texts = append(texts, extractContentText(msg["content"])...)
		if reasoning, ok := msg["reasoning_content"].(string); ok && reasoning != "" {
			texts = append(texts, reasoning)
		}
		texts = append(texts, extractToolCallsText(msg["tool_calls"])...)
	}
	return texts
}

func extractResponsesInputText(input any) []string {
	switch v := input.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		var texts []string
		for _, rawItem := range v {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			switch item["type"] {
			case "message", "":
				texts = append(texts, extractContentText(item["content"])...)
			case "input_text":
				if text, ok := item["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			case "function_call":
				if name, ok := item["name"].(string); ok && name != "" {
					texts = append(texts, name)
				}
				if fn, ok := item["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok && name != "" {
						texts = append(texts, name)
					}
					texts = append(texts, extractJSONText(fn["arguments"])...)
				}
				texts = append(texts, extractJSONText(item["arguments"])...)
			case "function_call_output":
				texts = append(texts, extractJSONText(item["output"])...)
			}
		}
		return texts
	default:
		return nil
	}
}

func extractContentText(content any) []string {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		var texts []string
		for _, rawBlock := range v {
			block, ok := rawBlock.(map[string]any)
			if !ok {
				continue
			}
			switch block["type"] {
			case "text", "thinking", "input_text", "output_text":
				if text, ok := block["text"].(string); ok && text != "" {
					texts = append(texts, text)
				}
			}
		}
		return texts
	default:
		return extractJSONText(v)
	}
}

func extractToolCallsText(toolCalls any) []string {
	arr, ok := toolCalls.([]any)
	if !ok {
		return nil
	}

	var texts []string
	for _, rawCall := range arr {
		call, ok := rawCall.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := call["id"].(string); ok && name != "" {
			// The call id is part of the request context and may appear in tool call payloads.
			texts = append(texts, name)
		}
		if name, ok := call["name"].(string); ok && name != "" {
			texts = append(texts, name)
		}
		if fn, ok := call["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				texts = append(texts, name)
			}
			texts = append(texts, extractJSONText(fn["arguments"])...)
		}
		texts = append(texts, extractJSONText(call["arguments"])...)
	}
	return texts
}

func extractJSONText(value any) []string {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case nil:
		return nil
	default:
		raw, err := json.Marshal(v)
		if err != nil || len(raw) == 0 {
			return nil
		}
		return []string{string(raw)}
	}
}

// ExtractOutputText extracts accumulated assistant output text from SSE streams
// or assembled JSON logs.
func ExtractOutputText(logResp []byte) string {
	if len(logResp) == 0 {
		return ""
	}

	var result string
	if isSSELog(logResp) {
		for _, evt := range protocol.ParseEvents(logResp) {
			if evt.Data == "" || evt.Data == "[DONE]" {
				continue
			}
			result += extractOutputTextFromJSON([]byte(evt.Data))
		}
		return result
	}

	return extractOutputTextFromJSON(logResp)
}

func isSSELog(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "event:") || strings.HasPrefix(trimmed, "data:")
}

func extractOutputTextFromJSON(body []byte) string {
	var result string

	if delta := gjson.GetBytes(body, "delta"); delta.Type == gjson.String && gjson.GetBytes(body, "type").String() == "response.output_text.delta" {
		result += delta.String()
	}

	for _, choice := range gjson.GetBytes(body, "choices").Array() {
		result += extractGJSONText(choice.Get("delta.reasoning_content"))
		result += extractGJSONText(choice.Get("delta.content"))
		result += extractGJSONText(choice.Get("message.reasoning_content"))
		result += extractGJSONText(choice.Get("message.content"))
	}

	result += extractResponsesOutputText(gjson.GetBytes(body, "output"))
	result += extractResponsesOutputText(gjson.GetBytes(body, "response.output"))
	return result
}

func extractGJSONText(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return value.String()
	}
	if value.IsArray() {
		var result string
		for _, item := range value.Array() {
			switch item.Get("type").String() {
			case "text", "thinking", "input_text", "output_text":
				result += item.Get("text").String()
			}
		}
		return result
	}
	return value.String()
}

func extractResponsesOutputText(output gjson.Result) string {
	var result string
	for _, item := range output.Array() {
		if item.Get("type").String() != "message" {
			continue
		}
		for _, content := range item.Get("content").Array() {
			switch content.Get("type").String() {
			case "output_text", "text":
				result += content.Get("text").String()
			}
		}
	}
	return result
}
