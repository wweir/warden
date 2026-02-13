# Warden - AI Gateway 实现计划

## 项目定位

Warden 是一个 AI 网关，核心能力是作为 LLM API 的反向代理，支持透明地向对话中注入 MCP 工具，拦截并执行工具调用，最终只将"干净"的 LLM 响应返回给客户端。

## 目录结构

```
├── cmd/warden/
│   └── main.go                  # 主入口
├── config/
│   ├── config.go                # 配置结构定义、Validate
│   └── warden.example.toml      # 配置示例
├── internal/
│   ├── app/
│   │   ├── gateway.go           # HTTP 网关服务，路由注册
│   │   └── interactive.go       # promptui 交互式工具管理
│   ├── gateway/
│   │   ├── proxy.go             # 通用请求转发（非 chat/completions）
│   │   ├── baseurl.go           # baseurl 选择、认证注入、fallback 逻辑
│   │   ├── chat.go              # chat/completions 请求处理核心
│   │   ├── stream.go            # SSE 流式响应缓冲、chunk 累积与重放
│   │   └── protocol.go          # 协议适配：OpenAI/Anthropic 流式 chunk 解析
│   ├── mcp/
│   │   └── client.go            # MCP client 实现（连接、工具发现、调用）
│   └── tool/
│       ├── injector.go          # tool_call 注入逻辑
│       └── executor.go          # 拦截 tool_call 响应并执行
├── pkg/
│   └── openai/
│       └── types.go             # OpenAI API 请求/响应类型定义
├── Makefile
└── go.mod
```

## 配置设计

```toml
addr = ":8080"

# BaseURL 统一配置，每个 baseurl 独立定义认证、超时、协议等
[baseurl.anthropic]
url = "https://api.anthropic.com"
protocol = "anthropic"           # 协议类型: openai, anthropic, ollama
api_key = "${ANTHROPIC_API_KEY}" # 认证密钥（支持环境变量展开）
timeout = "120s"                 # 请求超时

[baseurl.openai]
url = "https://api.openai.com"
protocol = "openai"
api_key = "${OPENAI_API_KEY}"
timeout = "60s"

[baseurl.openai-fast]
url = "https://api.openai.com"
protocol = "openai"
api_key = "${OPENAI_API_KEY}"
timeout = "30s"
default_model = "gpt-4o-mini"   # 可选：覆盖请求中的 model

[baseurl.local-ollama]
url = "http://localhost:11434"
protocol = "ollama"
timeout = "300s"

# 路由配置，每个路由前缀引用一个或多个 baseurl，并配置工具
[route."/anthropic"]
baseurls = ["anthropic"]         # 引用上面定义的 baseurl 名称
tools = ["filesystem", "web-search"]

[route."/openai"]
baseurls = ["openai", "openai-fast"]  # 多个 baseurl，按顺序 fallback 或由 model 路由
tools = ["filesystem"]

[route."/local"]
baseurls = ["local-ollama"]
tools = []

# MCP 工具配置
[mcp.filesystem]
command = "npx"
args = ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

[mcp.web-search]
command = "npx"
args = ["-y", "@anthropic/mcp-server-web-search"]
env = { ANTHROPIC_API_KEY = "${ANTHROPIC_API_KEY}" }
```

### baseurl 多选策略

当 route 配置了多个 baseurls 时，选择策略：

- **默认**：使用列表中第一个 baseurl
- **model 路由**：如果 baseurl 配置了 `default_model`，当请求中的 model 字段匹配时优先使用该 baseurl
- **fallback**：当上游返回 5xx 或连接失败时，依次尝试下一个 baseurl

## 核心模块实现

### 1. 配置模块 (`config/`)

**config.go**:

```go
type ConfigStruct struct {
    Addr    string                      `json:"addr" usage:"Gateway listening address"`
    BaseURL map[string]*BaseURLConfig   `json:"baseurl" usage:"Upstream LLM base URL configurations"`
    Route   map[string]*RouteConfig     `json:"route" usage:"Route prefix configurations"`
    MCP     map[string]*MCPConfig       `json:"mcp" usage:"MCP server configurations"`
}

type BaseURLConfig struct {
    Name         string          `json:"-"`             // populated from map key
    URL          string          `json:"url" usage:"Upstream LLM base URL"`
    Protocol     string          `json:"protocol" usage:"API protocol: openai, anthropic, ollama"`
    APIKey       deferlog.Secret `json:"api_key" usage:"API key for authentication"`
    Timeout      string          `json:"timeout" usage:"Request timeout (e.g. 60s, 2m)"`
    DefaultModel string          `json:"default_model" usage:"Default model override"`

    timeout time.Duration // parsed from Timeout
}

type RouteConfig struct {
    Prefix   string   `json:"-"`          // populated from map key
    BaseURLs []string `json:"baseurls" usage:"BaseURL names to use (order matters for fallback)"`
    Tools    []string `json:"tools" usage:"MCP tool names to inject"`

    enabledTools map[string]bool         // runtime state, 允许动态开关
}

type MCPConfig struct {
    Name    string            `json:"-"`       // populated from map key
    Command string            `json:"command" usage:"MCP server command"`
    Args    []string          `json:"args" usage:"MCP server arguments"`
    Env     map[string]string `json:"env" usage:"Environment variables"`
}
```

- `Validate()` 方法：
    - 校验每个 baseurl 的 URL 合法性、protocol 为已知值、timeout 可解析
    - 校验 route 中引用的 baseurls 名称在 `baseurl` 配置中存在
    - 校验 route 中引用的 tools 名称在 `mcp` 配置中存在
    - 校验路由前缀格式正确（以 `/` 开头）

### 2. 网关核心 (`internal/app/gateway.go`)

职责：HTTP 服务启动、路由注册。

```
路由规则：
  /<route_prefix>/*path  →  选择 baseurl，转发到 baseurl.url + /path

特殊处理：
  /<route_prefix>/*/chat/completions  →  进入 tool injection 流程
  其余请求                             →  直接透明转发
```

- 使用 `httprouter` 或 `http.ServeMux`（Go 1.22+ 支持路径参数）注册路由
- 每个路由前缀对应一个 handler，handler 内根据 route 配置的 baseurls 选择目标上游
- BaseURL 选择逻辑：遍历 baseurls 列表，优先匹配 `default_model`，否则使用第一个；失败时 fallback 到下一个
- 根据 baseurl 的 `protocol` 字段决定认证头格式（如 OpenAI 用 `Authorization: Bearer`，Anthropic 用 `x-api-key`）
- 根据 baseurl 的 `timeout` 设置请求 context deadline

### 3. 通用代理 (`internal/gateway/proxy.go`)

对非 `chat/completions` 的请求，直接透明转发：

- 根据 route 选择目标 baseurl
- Clone 请求头，注入 baseurl 的认证头
- 改写目标 URL（strip 路由前缀，拼接 baseurl.url）
- 设置 baseurl 配置的超时
- 转发请求体
- 复制响应头和响应体
- 上游失败时尝试 fallback 到下一个 baseurl

### 4. Chat Completions 处理 (`internal/gateway/chat.go`)

这是核心模块，处理流程：

```
客户端请求 → 读取请求体(JSON)
           → 注入 tools 定义到 request.tools[]
           → 转发给上游 LLM
           → 读取响应
           → 判断是否包含 tool_calls
              ├── 无 tool_call 或仅客户端自有工具 → 直接返回响应
              └── 包含注入的 tool_call →
                  ├── 执行注入的工具，获取结果
                  ├── 构造新请求（追加 tool_call message + tool result message）
                  ├── 递归调用 LLM（循环直到无注入工具调用）
                  └── 最终响应中过滤掉注入工具的 tool_call，只返回客户端原始工具和文本
```

关键设计点：

- **工具区分**：通过维护注入工具名称集合，区分"客户端原始工具"和"网关注入工具"
- **响应过滤**：最终返回给客户端时，从 `tool_calls` 数组中移除注入工具的调用
- **循环执行**：LLM 可能多次调用注入工具，需要循环处理直到 LLM 不再调用注入工具
- **超时控制**：整个循环设置最大轮次和总超时

### 5. SSE 流式响应处理 (`internal/gateway/stream.go`)

当客户端请求 `stream: true` 时，流式处理比非流式复杂得多，因为：

- tool_call 信息分散在多个 SSE chunk 中（function name 在第一个 chunk，arguments 分片在后续 chunk 中）
- 在流开始阶段无法预知 LLM 是否会调用注入的工具
- LLM 可能同时调用客户端工具和注入工具（混合 tool_call）
- OpenAI 和 Anthropic 的流式格式完全不同

#### OpenAI 流式 tool_call 格式

```
data: {"choices":[{"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_xxx","type":"function","function":{"name":"func_name","arguments":""}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"ar"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"g\":1}"}}]}}]}
data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}
data: [DONE]
```

关键点：`index` 字段标识并行 tool_call，`arguments` 需要跨 chunk 拼接，`finish_reason: "tool_calls"` 标识结束。

#### Anthropic 流式 tool_use 格式

```
event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_xxx","name":"func_name","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"arg\":1}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"}}
```

关键点：`content_block_start` 携带工具名，`input_json_delta` 分片传输参数，`stop_reason: "tool_use"` 标识结束。

#### 核心处理策略

由于流式场景下无法预知 LLM 是否会调用注入工具，采用**统一缓冲策略**：

```
客户端请求(stream:true) → 注入 tools → 转发给上游（始终 stream:true）
                        → 读取完整 SSE 流，累积所有 chunk
                        → 流结束后判断 finish_reason/stop_reason
  ├── 仅文本或仅客户端工具 → 重放已缓冲的 chunk 给客户端
  └── 包含注入工具的 tool_call →
      ├── 从累积的 chunk 中提取完整 tool_calls（按 index 拼接 arguments）
      ├── 分离：注入工具调用 vs 客户端工具调用
      ├── 执行注入工具，获取结果
      ├── 构造新请求（追加 messages），仍以 stream:true 发送
      ├── 循环直到无注入工具调用
      └── 最终一轮的 SSE 流：过滤注入工具相关 chunk 后转发给客户端
```

**为什么不能边读边转发再回退**：SSE 是单向流，一旦发送给客户端就无法撤回。如果先转发了文本 chunk，后续发现还有注入工具的 tool_call，已发送的内容无法收回，且网关需要执行工具后重新请求 LLM，会导致客户端收到不连续的响应。

**优化方向**：

- 缓冲带来的延迟主要是等待上游 LLM 完成生成，实际上非流式请求也有同样的等待时间
- 最终一轮（无注入工具调用时）的 SSE 流可以直接 pipe 转发，此时恢复真正的流式体验
- 如果 route 未配置任何 tools（`tools = []`），则完全跳过缓冲，直接 pipe 转发

### 6. MCP Client (`internal/mcp/client.go`)

职责：管理 MCP server 进程生命周期，提供工具发现和调用接口。

```go
type Client struct {
    name    string
    cmd     *exec.Cmd
    // MCP stdio transport
}

// Start 启动 MCP server 子进程，完成 initialize 握手
func (c *Client) Start(ctx context.Context) error

// ListTools 获取可用工具列表
func (c *Client) ListTools(ctx context.Context) ([]Tool, error)

// CallTool 调用指定工具
func (c *Client) CallTool(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error)

// Close 关闭连接，终止子进程
func (c *Client) Close() error
```

MCP 协议实现：

- 使用 stdio transport（stdin/stdout JSON-RPC）
- 实现 `initialize` → `initialized` 握手
- 实现 `tools/list` 获取工具定义
- 实现 `tools/call` 执行工具调用
- 考虑使用现有的 Go MCP SDK（如 `github.com/mark3labs/mcp-go`），避免重复造轮子

### 7. 工具注入 (`internal/tool/injector.go`)

职责：将 MCP 工具定义转换为 OpenAI function calling 格式，注入到请求中。

```go
// Inject 将 MCP 工具定义注入到 chat completion 请求的 tools 字段中
func Inject(req *openai.ChatCompletionRequest, tools []mcp.Tool) []string

// 返回值：注入的工具名称列表，用于后续区分
```

MCP Tool → OpenAI Tool 映射：

```
MCP Tool.name        → OpenAI function.name
MCP Tool.description → OpenAI function.description
MCP Tool.inputSchema → OpenAI function.parameters
```

### 8. 工具执行 (`internal/tool/executor.go`)

职责：检测响应中的注入工具调用，执行并返回结果。

```go
// Execute 检测并执行注入的 tool_calls，返回工具结果 messages
func Execute(ctx context.Context, toolCalls []openai.ToolCall, injectedTools []string, mcpClients map[string]*mcp.Client) ([]openai.Message, error)
```

### 9. OpenAI 类型定义 (`pkg/openai/types.go`)

定义 OpenAI Chat Completion API 的请求/响应结构体，仅包含网关需要操作的字段，其余用 `json.RawMessage` 保持透传：

```go
type ChatCompletionRequest struct {
    Model    string            `json:"model"`
    Messages []Message         `json:"messages"`
    Tools    []Tool            `json:"tools,omitempty"`
    Stream   bool              `json:"stream,omitempty"`
    Extra    map[string]json.RawMessage  // 其余字段透传
}

type Message struct {
    Role       string          `json:"role"`
    Content    any             `json:"content,omitempty"`
    ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
    Name       string          `json:"name,omitempty"`
}

type ToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"`
    Function FunctionCall `json:"function"`
}
```

注意：为了最大兼容性，对请求/响应 JSON 的未知字段应透传而非丢弃。使用自定义 `MarshalJSON`/`UnmarshalJSON` 实现。

### 10. 交互式工具管理 (`internal/app/interactive.go`)

使用 `promptui` 实现 CLI 交互界面：

```
$ warden -i

Select route:
> /anthropic
  /openai
  /local

Route: /anthropic (baseurls: anthropic)
Tools:
  [✓] filesystem::read_file
  [✓] filesystem::write_file
  [✓] web-search::search
  [ ] web-search::fetch

Toggle tool (space), Back (b), Quit (q):
```

功能：

- 选择路由前缀
- 显示该路由下所有可用工具（来自配置的 MCP servers）及其启用状态
- 支持开关单个工具
- 运行时动态生效（修改 `RouteConfig.enabledTools`）

## 实现顺序

| 阶段   | 内容                                                                                                               | 依赖    |
| ------ | ------------------------------------------------------------------------------------------------------------------ | ------- |
| **P1** | `config/`、`pkg/openai/types.go`、`cmd/warden/main.go`、`Makefile`                                                 | 无      |
| **P2** | `internal/gateway/baseurl.go`、`internal/gateway/proxy.go`、`internal/app/gateway.go` — baseurl 选择与透明代理能力 | P1      |
| **P3** | `internal/mcp/client.go` — MCP client，工具发现和调用                                                              | P1      |
| **P4** | `internal/tool/injector.go`、`internal/tool/executor.go` — 注入和执行                                              | P3      |
| **P5** | `internal/gateway/chat.go` — 非流式 chat completions 处理                                                          | P2 + P4 |
| **P6** | `internal/gateway/stream.go` — 流式 SSE 处理                                                                       | P5      |
| **P7** | `internal/app/interactive.go` — promptui 工具管理                                                                  | P3      |

## 第三方依赖说明

| 依赖                                  | 用途                                                |
| ------------------------------------- | --------------------------------------------------- |
| `github.com/julienschmidt/httprouter` | 轻量级 HTTP 路由，与 contatto 保持一致              |
| `github.com/sower-proxy/deferlog/v2`  | 函数退出日志，项目规范要求                          |
| `github.com/sower-proxy/feconf`       | 配置解析管理，项目规范要求                          |
| `github.com/lmittmann/tint`           | slog 彩色输出                                       |
| `github.com/manifoldco/promptui`      | 交互式 CLI 菜单                                     |
| `github.com/mark3labs/mcp-go`         | MCP 协议 Go SDK（避免重新实现 JSON-RPC + MCP 握手） |

## 关键设计决策

1. **BaseURL 分离配置**：将上游 LLM 的连接信息（URL、认证、超时、协议）独立为 `baseurl` 配置块，route 通过名称引用。好处：同一个 baseurl 可被多个 route 复用；同一个 route 可配置多个 baseurl 实现 fallback；认证信息集中管理。

2. **协议适配**：`baseurl.protocol` 字段决定认证头注入方式和 API 格式适配：
    - `openai`：`Authorization: Bearer <api_key>`
    - `anthropic`：`x-api-key: <api_key>` + `anthropic-version` header
    - `ollama`：无认证头

3. **JSON 透传**：请求/响应中网关不关心的字段必须原样透传，不能丢弃。使用 `json.RawMessage` + 自定义序列化实现。

4. **流式处理策略**：
    - route 无注入工具时：直接 pipe 转发，零额外延迟
    - route 有注入工具时：统一缓冲完整 SSE 流后判断是否有注入工具调用
    - 中间轮次（执行注入工具后重新请求）：始终缓冲
    - 最终一轮（无注入工具调用）：直接 pipe 转发，恢复真正的流式体验
    - 需分别实现 OpenAI（`delta.tool_calls[index].function.arguments` 拼接，`finish_reason: "tool_calls"`）和 Anthropic（`content_block_start` + `input_json_delta`，`stop_reason: "tool_use"`）两种流式 chunk 解析器

5. **工具名称空间**：MCP server 可能有同名工具。使用 `<mcp_name>::<tool_name>` 作为内部唯一标识，注入到 LLM 时使用 `<mcp_name>__<tool_name>` 格式（因为 OpenAI function name 不支持 `::`）。

6. **并发安全**：`RouteConfig.enabledTools` 需要 `sync.RWMutex` 保护，因为 interactive 模式可以运行时修改。

7. **错误恢复**：工具执行失败时，将错误信息作为 tool result 返回给 LLM，让 LLM 自行决定如何处理，而非中断整个请求。

8. **循环保护**：tool_call → execute → re-request 循环设置最大轮次（默认 10），防止无限循环。
