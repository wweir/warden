package gateway

import "fmt"

// GatewayError 是网关错误类型
type GatewayError interface {
	Error() string
	GetStatusCode() int
	IsRetryable() bool
}

// UpstreamError 表示上游 API 错误
type UpstreamError struct {
	Code int
	Body string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("upstream error: %d %s", e.Code, e.Body)
}

func (e *UpstreamError) GetStatusCode() int {
	return e.Code
}

func (e *UpstreamError) IsRetryable() bool {
	return e.Code >= 500 || e.Code == 429 // 5xx 或 429 可重试
}

// ProtocolError 表示协议转换错误
type ProtocolError struct {
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("protocol error: %s", e.Message)
}

func (e *ProtocolError) GetStatusCode() int {
	return 500
}

func (e *ProtocolError) IsRetryable() bool {
	return false
}

// ValidationError 表示请求验证错误
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s", e.Message)
}

func (e *ValidationError) GetStatusCode() int {
	return 400
}

func (e *ValidationError) IsRetryable() bool {
	return false
}

// ErrUnsupportedProtocol 不支持的协议
var ErrUnsupportedProtocol = &ProtocolError{
	Message: "unsupported protocol",
}
