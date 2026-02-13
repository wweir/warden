package gateway

import (
	"testing"
)

func TestProtocolFactory_Create(t *testing.T) {
	factory := &ProtocolFactory{}

	// 测试 OpenAI 协议
	openaiAdapter, err := factory.Create("openai")
	if err != nil {
		t.Fatalf("Failed to create OpenAI adapter: %v", err)
	}
	if _, ok := openaiAdapter.(*OpenAIAdapter); !ok {
		t.Errorf("Expected *OpenAIAdapter, got %T", openaiAdapter)
	}

	// 测试 Anthropic 协议
	anthropicAdapter, err := factory.Create("anthropic")
	if err != nil {
		t.Fatalf("Failed to create Anthropic adapter: %v", err)
	}
	if _, ok := anthropicAdapter.(*AnthropicAdapter); !ok {
		t.Errorf("Expected *AnthropicAdapter, got %T", anthropicAdapter)
	}

	// 测试 Ollama 协议
	ollamaAdapter, err := factory.Create("ollama")
	if err != nil {
		t.Fatalf("Failed to create Ollama adapter: %v", err)
	}
	if _, ok := ollamaAdapter.(*OllamaAdapter); !ok {
		t.Errorf("Expected *OllamaAdapter, got %T", ollamaAdapter)
	}

	// 测试不支持的协议
	_, err = factory.Create("unsupported")
	if err == nil {
		t.Error("Expected error for unsupported protocol")
	}
}

func TestBaseURLFactory_Create(t *testing.T) {
	factory := &BaseURLFactory{}

	// 测试基本功能
	baseURL, err := factory.Create("test", "openai")
	if err != nil {
		t.Fatalf("Failed to create BaseURL: %v", err)
	}
	if baseURL.GetName() != "test" {
		t.Errorf("Expected name 'test', got %s", baseURL.GetName())
	}
	if baseURL.GetProtocol() != "openai" {
		t.Errorf("Expected protocol 'openai', got %s", baseURL.GetProtocol())
	}

	// 测试 API 密钥
	_, err = factory.Create("test2", "anthropic", "sk-1234")
	if err != nil {
		t.Fatalf("Failed to create BaseURL with API key: %v", err)
	}

	// 测试超时
	_, err = factory.Create("test3", "ollama", 60)
	if err != nil {
		t.Fatalf("Failed to create BaseURL with timeout: %v", err)
	}
}

func TestRouterFactory_Create(t *testing.T) {
	factory := &RouterFactory{}

	// 测试基本功能
	router := factory.Create("/test", []string{"base1", "base2"})
	if router.Prefix != "/test" {
		t.Errorf("Expected prefix '/test', got %s", router.Prefix)
	}
	if len(router.BaseURLs) != 2 {
		t.Errorf("Expected 2 baseURLs, got %d", len(router.BaseURLs))
	}
	if len(router.Tools) != 0 {
		t.Errorf("Expected 0 tools, got %d", len(router.Tools))
	}

	// 测试工具
	routerWithTools := factory.Create("/api", []string{"base"}, []string{"web-search", "filesystem"})
	if len(routerWithTools.Tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(routerWithTools.Tools))
	}
	if !routerWithTools.EnabledTools["web-search"] {
		t.Error("web-search should be enabled")
	}
	if !routerWithTools.EnabledTools["filesystem"] {
		t.Error("filesystem should be enabled")
	}
}

func TestStrategy(t *testing.T) {
	ctx := &StrategyContext[int]{}

	// 加法策略
	ctx.SetStrategy(&AddStrategy{})
	result := ctx.Execute(41)
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}

	// 乘法策略
	ctx.SetStrategy(&MultiplyStrategy{})
	result = ctx.Execute(42)
	if result != 84 {
		t.Errorf("Expected 84, got %d", result)
	}
}

func TestDecorator(t *testing.T) {
	component := &ConcreteComponent{}

	decorated := &ConcreteDecoratorA{
		DecoratorBase{
			component: &ConcreteDecoratorB{
				DecoratorBase{
					component: component,
				},
			},
		},
	}

	result := decorated.DoSomething()
	if result != "A(B(Component))" {
		t.Errorf("Expected A(B(Component)), got %s", result)
	}
}

func TestOption_Chain(t *testing.T) {
	result := Some(5)
	if result.UnwrapOr(10) != 5 {
		t.Error("Expected 5")
	}

	result = None[int]()
	if result.UnwrapOr(10) != 10 {
		t.Error("Expected 10")
	}
}

func TestResult_Chain(t *testing.T) {
	// Ok chain
	res := Ok[int, string](42)
	if !res.IsOk() {
		t.Error("Expected Ok")
	}

	res = Err[int, string]("failed")
	if !res.IsErr() {
		t.Error("Expected Err")
	}
}

func TestToPairs(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2}
	pairs := ToPairs(m)
	if len(pairs) != 2 {
		t.Errorf("Expected 2 pairs, got %d", len(pairs))
	}

	// 转换回 map 应该相等
	m2 := ToMap(pairs)
	for k, v := range m {
		if m2[k] != v {
			t.Errorf("Expected %d for %s, got %d", v, k, m2[k])
		}
	}
}
