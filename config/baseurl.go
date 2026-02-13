package config

import (
	"net/http"
	"time"

	"warden/pkg/openai"
)

// BaseURL 是上游配置的抽象接口
type BaseURL interface {
	GetName() string
	GetURL() string
	GetProtocol() string
	GetTimeout() time.Duration
	GetDefaultModel() string
	CreateClient() (*http.Client, error)
	GetHeaders() map[string]string
}

// BaseURLImpl 是 BaseURL 的具体实现
type BaseURLImpl struct {
	Name            string
	URL             string
	Protocol        string
	APIKey          string
	Timeout         string
	DefaultModel    string
	TimeoutDuration time.Duration
}

// NewBaseURL 创建 BaseURLImpl 实例
func NewBaseURL(name string, url string, protocol string) *BaseURLImpl {
	return &BaseURLImpl{
		Name:     name,
		URL:      url,
		Protocol: protocol,
	}
}

func (b *BaseURLImpl) GetName() string {
	return b.Name
}

func (b *BaseURLImpl) GetURL() string {
	return b.URL
}

func (b *BaseURLImpl) GetProtocol() string {
	return b.Protocol
}

func (b *BaseURLImpl) GetTimeout() time.Duration {
	return b.TimeoutDuration
}

func (b *BaseURLImpl) GetDefaultModel() string {
	return b.DefaultModel
}

func (b *BaseURLImpl) CreateClient() (*http.Client, error) {
	return &http.Client{
		Timeout: b.TimeoutDuration,
	}, nil
}

func (b *BaseURLImpl) GetHeaders() map[string]string {
	headers := make(map[string]string)
	if b.APIKey != "" {
		headers["Authorization"] = "Bearer " + b.APIKey
	}
	headers["Content-Type"] = "application/json"
	return headers
}

// BaseURLBuilder 是构建模式
type BaseURLBuilder struct {
	baseURL *BaseURLImpl
}

func NewBaseURLBuilder(name string, url string, protocol string) *BaseURLBuilder {
	return &BaseURLBuilder{
		baseURL: NewBaseURL(name, url, protocol),
	}
}

func (b *BaseURLBuilder) WithAPIKey(apiKey string) *BaseURLBuilder {
	b.baseURL.APIKey = apiKey
	return b
}

func (b *BaseURLBuilder) WithTimeout(timeout string) *BaseURLBuilder {
	b.baseURL.Timeout = timeout
	// 解析超时
	dur, err := time.ParseDuration(timeout)
	if err == nil {
		b.baseURL.TimeoutDuration = dur
	} else {
		b.baseURL.TimeoutDuration = 60 * time.Second // 默认 60 秒
	}
	return b
}

func (b *BaseURLBuilder) WithDefaultModel(model string) *BaseURLBuilder {
	b.baseURL.DefaultModel = model
	return b
}

func (b *BaseURLBuilder) Build() BaseURL {
	return b.baseURL
}

// BaseURLManager 管理所有 BaseURL 配置
type BaseURLManager struct {
	baseURLs map[string]BaseURL
}

func NewBaseURLManager() *BaseURLManager {
	return &BaseURLManager{
		baseURLs: make(map[string]BaseURL),
	}
}

func (m *BaseURLManager) Add(baseURL BaseURL) {
	m.baseURLs[baseURL.GetName()] = baseURL
}

func (m *BaseURLManager) Get(name string) (BaseURL, bool) {
	b, ok := m.baseURLs[name]
	return b, ok
}

func (m *BaseURLManager) List() []BaseURL {
	list := make([]BaseURL, 0, len(m.baseURLs))
	for _, b := range m.baseURLs {
		list = append(list, b)
	}
	return list
}

func (m *BaseURLManager) Size() int {
	return len(m.baseURLs)
}

type RequestTransformer interface {
	Transform(request openai.ChatCompletionRequest) (interface{}, error)
}
