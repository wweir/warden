package gateway

import (
	"fmt"
	"sync"
	"warden/config"
)

// Factory 是通用工厂接口
type Factory[T any] interface {
	Create(key string, options ...interface{}) (T, error)
}

// ProtocolFactory 协议适配器工厂
type ProtocolFactory struct{}

func (f *ProtocolFactory) Create(protocol string, options ...interface{}) (Adapter[any], error) {
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

// BaseURLFactory BaseURL 创建工厂
type BaseURLFactory struct{}

func (f *BaseURLFactory) Create(name string, protocol string, options ...interface{}) (config.BaseURL, error) {
	builder := config.NewBaseURLBuilder(name, "https://api.example.com", protocol)

	// 处理可选参数
	for _, opt := range options {
		switch opt := opt.(type) {
		case string:
			// 当作 API 密钥处理
			builder.WithAPIKey(opt)
		case int:
			// 当作超时处理（秒）
			builder.WithTimeout(fmt.Sprintf("%ds", opt))
		}
	}

	return builder.Build(), nil
}

// RouterFactory 是路由创建工厂
type RouterFactory struct{}

func (f *RouterFactory) Create(prefix string, baseURLs []string, options ...interface{}) *config.RouteConfig {
	route := &config.RouteConfig{
		Prefix:       prefix,
		BaseURLs:     baseURLs,
		Tools:        []string{},
		EnabledTools: make(map[string]bool),
	}

	for _, opt := range options {
		if tools, ok := opt.([]string); ok {
			route.Tools = tools
			for _, tool := range tools {
				route.EnabledTools[tool] = true
			}
		}
	}

	return route
}

// Singleton 单例模式实现
type Singleton[T any] struct {
	instance T
	created  bool
	mu       sync.Mutex
}

func NewSingleton[T any](creator func() (T, error)) *Singleton[T] {
	return &Singleton[T]{
		created: false,
	}
}

func (s *Singleton[T]) Get() T {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.created {
		// 创建实例
		s.created = true
	}
	return s.instance
}

// Strategy 策略模式
type Strategy[T any] interface {
	Execute(data T) T
}

type AddStrategy struct{}

func (s *AddStrategy) Execute(data int) int {
	return data + 1
}

type MultiplyStrategy struct{}

func (s *MultiplyStrategy) Execute(data int) int {
	return data * 2
}

// StrategyContext 策略上下文
type StrategyContext[T any] struct {
	strategy Strategy[T]
}

func (c *StrategyContext[T]) SetStrategy(strategy Strategy[T]) {
	c.strategy = strategy
}

func (c *StrategyContext[T]) Execute(data T) T {
	return c.strategy.Execute(data)
}

// 装饰器模式
type Decorator interface {
	DoSomething() string
}

type ConcreteComponent struct{}

func (c *ConcreteComponent) DoSomething() string {
	return "Component"
}

type DecoratorBase struct {
	component Decorator
}

func (d *DecoratorBase) DoSomething() string {
	return d.component.DoSomething()
}

type ConcreteDecoratorA struct {
	DecoratorBase
}

func (d *ConcreteDecoratorA) DoSomething() string {
	return fmt.Sprintf("A(%s)", d.component.DoSomething())
}

type ConcreteDecoratorB struct {
	DecoratorBase
}

func (d *ConcreteDecoratorB) DoSomething() string {
	return fmt.Sprintf("B(%s)", d.component.DoSomething())
}

// 观察者模式
type Observer interface {
	Update(data interface{})
}

type Subject interface {
	RegisterObserver(o Observer)
	RemoveObserver(o Observer)
	NotifyObservers(data interface{})
}

type ConcreteSubject struct {
	observers []Observer
}

func (s *ConcreteSubject) RegisterObserver(o Observer) {
	s.observers = append(s.observers, o)
}

func (s *ConcreteSubject) RemoveObserver(o Observer) {
	idx := -1
	for i, obs := range s.observers {
		if obs == o {
			idx = i
			break
		}
	}
	if idx != -1 {
		s.observers = append(s.observers[:idx], s.observers[idx+1:]...)
	}
}

func (s *ConcreteSubject) NotifyObservers(data interface{}) {
	for _, o := range s.observers {
		o.Update(data)
	}
}

// 命令模式
type Command interface {
	Execute()
}

type ConcreteCommand struct {
	receiver *Receiver
}

func (c *ConcreteCommand) Execute() {
	c.receiver.Action()
}

type Receiver struct{}

func (r *Receiver) Action() {
	// 执行操作
}

type Invoker struct {
	command Command
}

func (i *Invoker) SetCommand(command Command) {
	i.command = command
}

func (i *Invoker) Execute() {
	if i.command != nil {
		i.command.Execute()
	}
}
