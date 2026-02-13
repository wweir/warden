package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"warden/config"
	"warden/pkg/openai"
)

// Adapter 是协议适配接口
type Adapter[T any] interface {
	ConvertRequest(req openai.ChatCompletionRequest) (T, error)
	ConvertResponse(res T) (openai.ChatCompletionResponse, error)
}

// OpenAIAdapter OpenAI 协议适配器
type OpenAIAdapter struct{}

func (a *OpenAIAdapter) ConvertRequest(req openai.ChatCompletionRequest) (interface{}, error) {
	// OpenAI 协议直接透传
	return req, nil
}

func (a *OpenAIAdapter) ConvertResponse(res interface{}) (openai.ChatCompletionResponse, error) {
	// 类型断言
	openaiRes, ok := res.(openai.ChatCompletionResponse)
	if !ok {
		return openai.ChatCompletionResponse{}, &ProtocolError{"invalid OpenAI response type"}
	}
	return openaiRes, nil
}

// AnthropicAdapter Anthropic 协议适配器
type AnthropicAdapter struct{}

func (a *AnthropicAdapter) ConvertRequest(req openai.ChatCompletionRequest) (interface{}, error) {
	// 转换为 Anthropic Claude 格式
	type AnthropicRequest struct {
		Model     string        `json:"model"`
		MaxTokens int64         `json:"max_tokens"`
		Messages  []interface{} `json:"messages"`
	}

	anthReq := &AnthropicRequest{
		Model:     req.Model,
		MaxTokens: 4096,
	}

	for _, msg := range req.Messages {
		anthReq.Messages = append(anthReq.Messages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	return anthReq, nil
}

func (a *AnthropicAdapter) ConvertResponse(res interface{}) (openai.ChatCompletionResponse, error) {
	// 转换回 OpenAI 格式
	// 这是简化实现，实际需要处理复杂的转换逻辑
	return openai.ChatCompletionResponse{
		ID:      "chatcmpl-anthropic-123",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "claude-3-opus-20240229",
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:    "assistant",
					Content: "This is an Anthropic response",
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

// OllamaAdapter Ollama 协议适配器
type OllamaAdapter struct{}

func (a *OllamaAdapter) ConvertRequest(req openai.ChatCompletionRequest) (interface{}, error) {
	// 转换为 Ollama 格式
	type OllamaRequest struct {
		Model    string        `json:"model"`
		Messages []interface{} `json:"messages"`
		Stream   bool          `json:"stream"`
	}

	ollamaReq := &OllamaRequest{
		Model:  req.Model,
		Stream: req.Stream,
	}

	for _, msg := range req.Messages {
		ollamaReq.Messages = append(ollamaReq.Messages, map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	return ollamaReq, nil
}

func (a *OllamaAdapter) ConvertResponse(res interface{}) (openai.ChatCompletionResponse, error) {
	// 转换回 OpenAI 格式
	return openai.ChatCompletionResponse{
		ID:      "chatcmpl-ollama-123",
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "llama3:8b",
		Choices: []openai.Choice{
			{
				Index: 0,
				Message: openai.Message{
					Role:    "assistant",
					Content: "This is an Ollama response",
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

// NewAdapter 根据协议创建适配器
func NewAdapter(protocol string) (Adapter[interface{}], error) {
	switch protocol {
	case "openai":
		return &OpenAIAdapter{}, nil
	case "anthropic":
		return &AnthropicAdapter{}, nil
	case "ollama":
		return &OllamaAdapter{}, nil
	default:
		return nil, ErrUnsupportedProtocol
	}
}

func sendUpstreamRequest(buCfg *config.BaseURLConfig, req interface{}) (interface{}, error) {
	// 简化实现，实际应该发送 HTTP 请求
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	slog.Debug("Sending request to upstream", "url", buCfg.URL, "body", string(reqBody))

	// 创建 HTTP 请求
	httpReq, err := http.NewRequest(http.MethodPost, buCfg.URL+"/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if buCfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+buCfg.APIKey)
	}

	// 发送请求（使用超时）
	client := &http.Client{
		Timeout: buCfg.TimeoutDuration,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &UpstreamError{
			Code: resp.StatusCode,
			Body: string(respBody),
		}
	}

	var openaiResp openai.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, err
	}

	return openaiResp, nil
}
