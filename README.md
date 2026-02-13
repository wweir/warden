# Warden - AI Gateway

Warden 是一个现代化的 AI Gateway，采用 Go 1.23+ 的泛型和面向对象编程技术构建。它提供统一的接口管理和工具注入功能，支持动态工具注入、模型路由和 MCP（Model Context Protocol）集成。

## 技术特色

### 泛型编程 (Go 1.18+)
- 类型安全的通用数据结构（Option、Result）
- 可复用的泛型函数（Map、Filter、Find、Reduce）
- 类型参数化的工厂和策略模式

### 面向对象设计
- 接口抽象与依赖注入
- 装饰器和观察者模式
- 命令和策略模式
- 完整的继承和多态支持

### 工程化最佳实践
- 可测试的架构设计
- 详细的测试覆盖率
- 统一的错误处理
- 优雅的关闭机制

## 核心功能

### 1. 统一 API 入口
- 支持 OpenAI、Anthropic、Ollama 等多种 LLM 协议
- 统一的 OpenAI 格式请求/响应格式
- 按前缀路由的请求转发

### 2. 动态工具注入
- MCP (Model Context Protocol) 工具支持
- 动态开关工具可用性
- 工具权限管理

### 3. 智能路由与容错
- 按前缀匹配的路由系统
- 基于模型名称的路由
- 备用 BaseURL 故障转移
- 请求超时和重试机制

### 4. 配置化管理
- TOML 格式配置文件
- 支持环境变量展开
- 动态重新加载配置

## 快速开始

### 安装

```bash
# 使用 Makefile 构建（推荐）
make build

# 直接编译
go build -o bin/warden ./cmd/warden

# 查看版本信息
./bin/warden --version
```

### 使用 Docker

```bash
# 构建 Docker 镜像
make docker-build

# 运行 Docker 容器
make docker-run
```

### 配置

创建 `warden.toml` 配置文件（可参考 `config/warden.example.toml`）：

```toml
addr = ":8080"

[baseurl.openai-test]
url = "https://api.openai.com"
protocol = "openai"
api_key = "${OPENAI_API_KEY}"
timeout = "60s"

[route."/openai"]
baseurls = ["openai-test"]
tools = []

[mcp.web-search]
command = "npx"
args = ["-y", "@anthropic/mcp-server-web-search"]
env = { ANTHROPIC_API_KEY = "${ANTHROPIC_API_KEY}" }
```

### 运行

```bash
# 使用 Makefile
make run

# 直接运行
./bin/warden
```

## 使用方法

### 1. API 调用

Warden 提供与 OpenAI 兼容的接口：

```bash
curl http://localhost:8080/openai/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

## 项目结构

```
warden/
├── cmd/warden/          # 命令行入口（包含版本信息）
├── config/              # 配置系统
│   ├── config.go       # 配置解析和验证
│   └── warden.example.toml # 示例配置
├── internal/            # 内部包（不公开）
│   ├── app/            # 应用主逻辑（生命周期管理）
│   ├── gateway/        # 核心网关处理
│   │   ├── adapter.go  # 协议适配器
│   │   ├── errors.go   # 错误处理
│   │   ├── factory.go  # 工厂和策略模式
│   │   ├── gateway.go  # HTTP 服务器
│   │   ├── middleware.go # 请求中间件
│   │   ├── typeparam.go # 泛型工具函数
│   │   └── *.test.go  # 测试文件
│   └── mcp/            # MCP 管理
├── pkg/                 # 公共包（公开 API）
│   └── openai/         # OpenAI 格式定义
├── Makefile             # 工程化构建脚本
├── README.md            # 项目文档
├── go.mod/go.sum       # 依赖管理
└── .gitignore          # Git 忽略配置
```

## 开发

### 代码质量保证

```bash
# 运行全部测试
make test-all

# 测试覆盖率
make coverage
make coverage-html

# 静态检查
make vet

# 代码格式化
make fmt

# 代码审查
make lint
```

### 性能测试

```bash
# 运行基准测试
make bench

# 查看性能报告
go tool pprof http://localhost:6060/debug/pprof/profile
```

### 文档

```bash
# 生成文档
make doc

# 运行文档服务器
godoc -http=:6060
```

## 架构设计

### 设计模式应用

#### 1. 泛型编程
```go
// Result 表示通用的结果类型
type Result[T any] struct {
    Value T
    Err   error
}

// Map 是通用的转换函数
func Map[T, U any](slice []T, f func(T) U) []U {
    result := make([]U, len(slice))
    for i, v := range slice {
        result[i] = f(v)
    }
    return result
}
```

#### 2. 工厂和策略模式
```go
// ProtocolFactory 创建不同协议的适配器
type ProtocolFactory struct{}

func (f *ProtocolFactory) Create(protocol string) (Adapter, error) {
    switch protocol {
    case "openai":
        return &OpenAIAdapter{}, nil
    case "anthropic":
        return &AnthropicAdapter{}, nil
    default:
        return nil, ErrUnsupportedProtocol
    }
}
```

#### 3. 观察者模式
```go
// Subject 是被观察对象
type Subject interface {
    RegisterObserver(o Observer)
    RemoveObserver(o Observer)
    NotifyObservers(data interface{})
}

// Observer 是观察者
type Observer interface {
    Update(data interface{})
}
```

## 许可证

MIT License

