# Warden - AI Gateway 实现计划

## 项目定位

Warden 是一个 AI 网关，核心能力是作为 LLM API 的反向代理，支持透明地向对话中注入 MCP 工具，拦截并执行工具调用，最终只将"干净"的 LLM 响应返回给客户端。

## 目录结构

```
├── cmd/warden/
│   └── main.go                  # 主入口：默认启动 proxy，-s 安装服务
├── config/
│   ├── config.go                # 配置结构定义、Validate、ExampleConfig embed
│   └── warden.example.yaml      # 配置示例（YAML 唯一格式）
├── web/
│   ├── embed.go                 # embed.FS 暴露前端静态文件
│   └── admin/                   # Vue 3 + Vite 前端项目
│       ├── README.md            # 管理前端职责、Dashboard 数据流、构建方式
│       ├── dist/                # 构建产物（提交到 git）
│       └── src/                 # 源码：Dashboard / Config / Logs / ProviderDetail / McpDetail / McpToolDetail / ToolHooks 页面
├── internal/
│   ├── app/
│   │   └── app.go               # App 结构体、HTTP 服务启动、graceful shutdown（SIGTERM 触发进程退出）
│   ├── install/
│   │   └── service.go           # systemd 服务安装/更新
│   ├── gateway/
│   │   ├── README.md            # Gateway/Admin API/指标流职责说明
│   │   ├── gateway.go           # Gateway 核心：路由注册、MCP 管理、provider 选择、代理
│   │   ├── api_admin.go         # Admin API：REST + SSE 日志流/指标流 + Basic Auth + 配置管理 + 热重载 + Provider 探活/详情
│   │   ├── api_chat.go          # Chat Completions 请求处理（透传优化 + 工具拦截）
│   │   ├── api_responses.go     # Responses API 请求处理（透传优化 + 工具拦截）
│   │   ├── adapter.go           # 协议适配：endpoint 路由、请求/响应序列化、流式 parser 工厂
│   │   ├── dashboard_metrics.go # 仪表盘实时指标滚动缓存：Prometheus 累计指标 + 新鲜输出速率缓存 -> 时序点（空闲后输出速率自动归零）
│   │   ├── http.go              # upstream HTTP 通信：sendRequest、pipeRawStream、认证注入、模型拉取
│   │   ├── convert.go           # 公共转换辅助函数：tool call/result 类型转换、分离（泛型实现）、过滤
│   │   ├── selector.go          # Provider 多选策略：配置顺序 + 模型匹配 + 失败抑制 + 滑动窗口统计 + 状态暴露
│   │   ├── middleware.go        # HTTP 中间件：日志、panic 恢复、CORS
│   │   ├── metrics.go           # Prometheus 指标：requests_total、request_duration_ms、stream_ttft_ms、completion_throughput_tps、provider_health、provider_suppressed、tokens_total、token_rate（按 route/provider/model/endpoint 维度统计，可用于 P95 TTFT / P99 Throughput）
│   │   └── errors.go            # 错误类型：UpstreamError、ErrProviderNotFound
│   ├── reqlog/
│   │   ├── reqlog.go            # Logger 接口、Record/Step 类型定义、BuildFingerprint、内部 JSON 提取函数（gjson）
│   │   ├── file.go              # FileLogger：将日志写入 JSON 文件（按路由+时间戳命名）
│   │   ├── http.go              # HTTPLogger：异步推送日志到 HTTP 端点，支持模板渲染（sprig）
│   │   ├── broadcast.go         # Broadcaster：内存广播器，SSE 订阅者推送 + 最近 50 条环形缓冲
│   │   └── logger.go            # newLogger/multiLogger：按配置构建多后端 Logger
│   ├── toolexec/
│   │   └── tool_exec.go         # 工具执行：对任意 tool call 触发 hook，注入 MCP tool 由网关执行
│   └── mcp/
│       └── client.go            # MCP client 实现（JSON-RPC stdio、工具发现、调用）
├── pkg/
│   ├── protocol/
│   │   ├── sse.go               # LLM 协议公共类型：Event、ToolCallInfo、StreamParser 接口、SSE 解析/重放
│   │   ├── openai/
│   │   │   ├── types.go         # OpenAI Chat Completions API 请求/响应类型定义
│   │   │   ├── responses.go     # OpenAI Responses API 请求/响应类型定义
│   │   │   ├── convert.go       # Chat↔Responses 协议转换器（chat_to_responses / responses_to_chat）
│   │   │   ├── stream.go        # OpenAI SSE 流式解析器（Chat + Responses）
│   │   │   ├── inject.go        # 工具注入（Chat Completions + Responses API）
│   │   │   └── prompt.go        # 系统提示词注入（Chat Completions + Responses API）
│   │   └── anthropic/
│   │       ├── anthropic.go     # Anthropic 协议适配：OpenAI ↔ Anthropic 格式转换
│   │       ├── stream.go        # Anthropic SSE 流式解析器
│   │       └── auth.go          # Anthropic 认证头设置
│   ├── provider/
│   │   ├── provider.go          # TokenProvider 接口定义、Get() 工厂函数
│   │   ├── qwen.go              # Qwen OAuth token 管理（自动刷新，支持 SSH 远程读取）
│   │   └── copilot.go           # GitHub Copilot token 管理（自动刷新，支持 SSH 远程读取）
│   ├── toolhook/
│   │   ├── hook.go              # 通用 tool hook 调度：规则匹配、pre/post 并发执行、拒绝处理
│   │   ├── exec.go              # exec hook：stdin 传入 CallContext JSON，stdout 解析 allow/reason
│   │   ├── ai.go                # ai hook：调用网关 chat/completions，解析 allow/reason
│   │   └── http.go              # http hook：调用 webhook 配置，解析 allow/reason
│   ├── ssh/
│   │   └── ssh.go               # SSH 工具包：远程命令执行、文件读取（shell out to system ssh）
│   └── protocol/
│       └── protocol.go          # LLM 协议公共类型：Event、ToolCallInfo、StreamParser 接口、SSE 解析/重放
├── Makefile
└── go.mod
```

## 配置设计

配置使用 YAML 格式（唯一支持格式），完整示例参见 [`config/warden.example.yaml`](config/warden.example.yaml)。

主要配置块：`addr`、`admin_password`、`webhook`、`log`、`ssh`、`provider`、`route`、`mcp`、`tool_hooks`。Provider 支持 openai/anthropic/ollama/qwen/copilot 五种协议，API key 支持 `${ENV_VAR}` 环境变量展开。Provider 可配置 `chat_to_responses: true`（仅限 `protocol: "openai"`），将客户端 `chat/completions` 请求转换为 Responses API 格式发送到上游 `/responses`；也可配置 `responses_to_chat: true`（仅限 `protocol: "openai"`），将客户端 `responses` 请求转换为 Chat Completions 格式发送到上游 `/chat/completions`。后者仅支持 Chat 兼容子集：字符串/数组 `input`、`function` tools；`previous_response_id`、`web_search`、`file_search` 等 Responses 原生能力无法映射。MCP 工具可通过 `tools.<name>.disabled` 禁用；`tool_hooks` 可对任意 tool call（注入 MCP 工具名为 `mcp__tool`，客户端工具名为原始 name）配置 exec/ai/http 类型的 pre/post hook。

### provider 多选策略 (internal/gateway/selector.go)

当 route 配置了多个 providers 时，选择策略如下，优先级从高到低：

1. **配置顺序**（主要选择逻辑）：按 `RouteConfig.Providers` 的顺序遍历所有 provider（第一个 = 最高优先级）。当请求指定了 model 时，通过 `availableModels`（来自 `GET /models`）过滤不支持该 model 的 provider。跳过被抑制的 provider。

2. **手动抑制**：管理员可通过 Admin API 或管理面板手动抑制某个 provider，被手动抑制的 provider 会被完全跳过，不参与选择。手动抑制是运行时生效的，重启后重置。

3. **全部被抑制时的兜底**：如果所有 provider 都被抑制（`suppressUntil > now`），则返回抑制期最早结束的那个，以保证服务可用。注意：手动抑制的 provider 不参与此兜底逻辑；若候选集中只剩手动抑制 provider，则直接返回 `ErrProviderNotFound`，Failover 会停止而不会切回手动抑制节点。

#### 失败抑制机制

通过 `RecordOutcome` 方法收集每个请求的结果和延迟，对失败的 provider 执行指数退避抑制：

- **成功**：立即重置 `consecutiveFailures=0`，清除 `suppressUntil`
- **400/401/403/404/429、5xx 或连接错误**：计入失败；其中 401 会先触发一次凭据刷新重试，若仍失败则参与 failover 和抑制。连续失败 N 次后，`suppressUntil = now + 30s * 2^(N-1)`
- **最大抑制期**：当连续失败 >=5 次时，抑制期稳定在 ~480 秒（8 分钟）

**核心实现：**

- `internal/gateway/selector.go` — Selector 结构体，Providers Order + Model Match 选择
- `providerState` — 内部状态跟踪：`consecutiveFailures`, `suppressUntil`, `totalRequests`, `successCount`, `failureCount`, `totalLatencyMs`
- `RecordOutcome` — 结果记录与抑制逻辑（包含延迟统计）
- `NewSelector` — 初始化所有 provider 的状态

#### 模型发现 (Model Discovery)

服务启动时，Gateway 异步调用 `RefreshModels()`，Selector 并行查询每个 provider 的 `GET /models` 端点，获取其实际可用的模型列表，存储在 `providerState.availableModels` 中。若 provider 配置了 `models` 字段，这些模型会先作为基线立即生效，随后仍然尝试远程发现；远程请求成功后，发现的模型与配置的模型合并（去重）；远程请求失败时仅保留配置的模型。模型发现异步执行，不阻塞服务启动；拉取失败仅记录日志，不影响服务运行。

在候选构建阶段，如果请求指定了 model 且 provider 的 `availableModels` 不为 nil，则只保留包含该 model（或定义了该 model 别名）的 provider 作为候选。这确保了当一个 route 配置了多个不同 provider（如 openai + anthropic）时，请求会被路由到实际支持该模型的 provider。

**降级策略**：如果某个 provider 的 `/models` 查询失败（网络错误、认证问题等），`availableModels` 保持 nil，该 provider 不会被模型过滤排除，按原有配置顺序逻辑参与选择。

**实现要点**：

- `fetchModels(provCfg)` — 发送 GET 请求，解析 `{"data":[{"id":"..."}]}` 通用格式，同时返回 `map[string]bool`（用于 Select 过滤）和 `[]json.RawMessage`（原始模型对象，用于聚合端点）
- 支持 Anthropic 分页（`has_more` + `after_id`）
- 查询使用 30s 固定超时（启动时一次性操作）
- 并行查询所有 provider（goroutine + WaitGroup）

#### 模型列表聚合端点

`GET /{prefix}/models` 返回同一 route 下所有 provider 的模型列表合并结果，按 model ID 去重。每个模型的 `owned_by` 字段被设置为对应的 provider 名称。

- `Selector.Models(cfg, route)` — 遍历 route 的 providers，收集每个 provider 的 `rawModels`，注入 `owned_by` 字段，按 ID 去重后返回；同时将 `model_aliases` 中定义的别名作为额外的模型条目添加到结果中（包含 `aliased` 字段指向真实模型名）
- 响应格式为 OpenAI 标准格式：`{"object": "list", "data": [...]}`
- 如果所有 provider 的模型查询均失败，返回空 data 数组

#### 模型别名 (Model Aliases)

provider 可配置 `model_aliases` 映射（配置示例参见 `warden.example.yaml` 中的 `model_aliases` 字段），允许一个模型以别名暴露给客户端：

- 别名出现在 `GET /models` 响应中，客户端可直接使用别名作为 model 名称
- `Select` 时，别名也参与候选匹配——请求 alias model 会选中定义了该别名的 provider
- `ResolveModel()` 将别名解析为真实模型名，在发送到上游前调用

## 核心模块实现

### 1. 配置模块 (`config/`)

配置结构定义在 `config/config.go`，YAML 字段与 [`config/warden.example.yaml`](config/warden.example.yaml) 一一对应。

- `Validate()` 方法：
    - 校验每个 provider 的 URL 合法性、protocol 为已知值、timeout 可解析
    - 校验全局 `tool_hooks` 每条规则的 `match` 非空、hook 的 type/when 必填；exec 需 command、ai 需 route+model+prompt、http 需 webhook（引用 `webhook` 配置）；timeout 可解析
    - 校验 route 中引用的 providers 名称在 `provider` 配置中存在
    - 校验 route 中引用的 tools 名称在 `mcp` 配置中存在
    - 校验路由前缀格式正确（以 `/` 开头）

### 2. 网关核心 (`internal/gateway/gateway.go`)

职责：Gateway 结构体，路由注册、MCP 客户端管理、**Selector 集成**、通用代理。

- `NewGateway()` 初始化时启动所有 MCP 客户端，创建 Selector，注册路由，组装中间件链
- `selectProvider()` 委托到 `selector.Select`，支持 providers order + model match + 失败抑制
- `recordOutcome()` 辅助函数，用于调用后结果记录
- `handleProxy()` 对非 chat/responses 请求透明转发：先 clone 请求头，再清洗 hop-by-hop/客户端认证/伪造转发头，随后重建 `X-Forwarded-*` 并覆盖 provider 认证头后转发。该路径会基于客户端 `Accept-Encoding` 协商上游压缩（推理端点优先 `zstd`，其次 `br/gzip`），并在上游返回压缩响应时跳过 body 级错误解析与 token 统计，避免把二进制压缩体当作 JSON 解析。**注意**：透明代理路径不解析请求结构，缺少 MCP 工具注入和 System Prompt 注入能力；Anthropic `/messages` 走此路径
- `Close()` 优雅关闭所有 MCP 客户端

**删除的旧实现：** 不再使用 `checkProvider()` 进行 HTTP HEAD 健康检查（过于频繁且浪费），改为被动健康感知。

### 2.1. 协议适配 (`internal/gateway/adapter.go`)

职责：统一协议差异，对上层屏蔽 OpenAI/Anthropic 的格式区别。

- `protocolEndpoint(protocol, isResponses)` — 统一 endpoint 路由（Chat Completions / Responses / Anthropic Messages）
- `marshalProtocolRequest(protocol, req)` — 将 OpenAI 格式请求序列化为目标协议格式
- `marshalProtocolRaw(protocol, rawBody)` — 无工具注入时直接透传 raw bytes（OpenAI/Ollama 零拷贝，Anthropic 需解析转换）
- `unmarshalProtocolResponse(protocol, body)` — 将目标协议响应反序列化为 OpenAI 格式
- `newStreamParser(protocol, isResponses)` — 根据协议和 API 类型创建对应的 SSE 流式解析器

### 2.2. Upstream 通信 (`internal/gateway/http.go`)

职责：封装所有与上游 LLM API 的 HTTP 通信。

- `setAuthHeaders(h, provCfg)` — 根据协议注入认证头（OpenAI: Bearer, Anthropic: x-api-key）
- `sendRequest(ctx, provCfg, endpoint, body)` — 发送 POST 请求，返回 raw response body + 首-token 延迟；当上下文携带原始客户端请求时复用统一 header 逻辑（复制+清洗+重建 `X-Forwarded-*` + 认证覆盖）后转发到上游
- `pipeRawStream(ctx, w, provCfg, endpoint, body)` — 发送请求并直接 pipe 响应到客户端
- 所有上游 HTTP 请求通过 `ProviderConfig.HTTPClient()` 创建客户端，自动配置代理和超时

**超时策略**：使用 `Transport.ResponseHeaderTimeout`（首-token 超时）而非 `http.Client.Timeout`（全请求超时）。这确保 streaming 响应不会因总时间长而被强制终止，同时仍能在上游无响应时及时失败。延迟统计记录从请求发出到收到响应头的时间（首-token 延迟），而非包含 body 读取的总时间。

### 2.3. HTTP 中间件 (`internal/gateway/middleware.go`)

- `LoggingMiddleware` — 记录请求 method/path/remote_addr
- `RecoveryMiddleware` — panic 恢复，返回 500
- `CORS` — 跨域头设置
- `Chain()` — 组合多个中间件

### 4. Chat Completions 处理 (`internal/gateway/chat.go`)

这是核心模块，处理流程：

```
客户端请求 → ReadAll 原始 body bytes
           → 轻量解析（仅提取 model、stream 字段）
           → 选择 provider
           → 检查 route 是否配置了 MCP 工具
              ├── 无工具注入 → 透传 raw bytes 到上游（OpenAI/Ollama 零拷贝，Anthropic 需协议转换）
              │                → 透传 raw response bytes 给客户端
              └── 有工具注入 → 完整 Decode 请求体为 ChatCompletionRequest struct
                             → 注入 tools 定义到 request.tools[]
                             → 转发给上游 LLM
                             → 解析响应，同时保留 raw response bytes
                             → 判断是否包含 tool_calls
                                ├── 无注入工具调用 → 直接透传 raw response bytes
                                └── 包含注入工具调用 →
                                    ├── 执行注入的工具，获取结果
                                    ├── 构造新请求（追加 tool_call message + tool result message）
                                    ├── 循环调用 LLM（最多 10 轮）
                                    └── 最终响应中过滤注入工具的 tool_calls
```

关键设计点：

- **工具区分**：通过维护注入工具名称集合，区分"客户端原始工具"和"网关注入工具"
- **响应过滤**：最终返回给客户端时，从 `tool_calls` 数组中移除注入工具的调用
- **循环执行**：LLM 可能多次调用注入工具，需要循环处理直到 LLM 不再调用注入工具
- **超时控制**：整个循环设置最大轮次和总超时
- **混合工具调用**：当 LLM 在同一轮同时调用了客户端工具和注入工具时，网关执行注入工具后，需要将注入工具的 tool result 和客户端工具的 tool_call 一起构造新请求继续循环。但客户端工具缺少 tool result 会导致 LLM 报错。因此，混合调用时网关应**中断循环**：执行完注入工具后，构造响应返回给客户端，响应中仅保留客户端工具的 tool_calls（过滤掉注入工具的 tool_calls），让客户端处理自有工具后再发下一轮请求。如果下一轮 LLM 又调用了注入工具，网关继续拦截执行。

### 4.5. Responses API 处理 (`internal/gateway/responses.go`)

处理 Responses API（`POST /*/responses`），核心流程与 Chat Completions 类似，但请求/响应格式完全不同。

> 注：虽然 Responses API 最初由 OpenAI 定义，但 Open Responses 规范已被 Ollama、vLLM、LM Studio 等采纳。网关不限制 protocol 类型——任何 route 的请求路径匹配 `*/responses` 都会进入此处理流程，由上游自行决定是否支持。

#### Responses API 与 Chat Completions 的关键差异

| 维度            | Chat Completions                                 | Responses API                                                    |
| --------------- | ------------------------------------------------ | ---------------------------------------------------------------- |
| 端点            | `POST /v1/chat/completions`                      | `POST /v1/responses`                                             |
| 输入结构        | `messages[]` (Message 数组)                      | `input[]` (Item 数组，或纯字符串)                                |
| 工具定义        | `tools[].function.{name,description,parameters}` | `tools[].{type:"function",name,description,parameters}` (更扁平) |
| 工具调用输出    | `choices[].message.tool_calls[]`                 | `output[]` 中的 `function_call` items                            |
| 工具结果回传    | `messages[]` 中添加 `role:"tool"` message        | `input[]` 中添加 `function_call_output` item                     |
| 工具调用标识    | `tool_call.id`                                   | `function_call.call_id`                                          |
| 流式格式        | `data: {"choices":[{"delta":...}]}`              | 语义化事件: `response.function_call_arguments.delta` 等          |
| 流式结束标记    | `finish_reason: "tool_calls"`                    | `response.completed` 事件 + output item 的 `status`              |
| 上下文管理      | 客户端手动管理完整 messages                      | 可用 `previous_response_id` 服务端管理                           |
| reasoning items | 不涉及                                           | 推理模型会返回 reasoning items，工具调用时需回传                 |

#### 处理流程

```
客户端请求(POST /*/responses) → ReadAll 原始 body bytes
    → 轻量解析（仅提取 model、stream 字段）
    → 选择 provider
    → 检查 route 是否配置了 MCP 工具
       ├── 无工具注入 → 透传 raw bytes 到上游
       │                → 透传 raw response bytes 给客户端（流式/非流式均适用）
       └── 有工具注入 → 完整 Decode 请求体为 ResponsesRequest struct
           → 注入 MCP 工具定义到 tools[]（Responses API 扁平格式）
           → 透传所有未识别字段（model, instructions, store, temperature 等）
           → 转发给上游
           → 解析响应，同时保留 raw response bytes
           → 轻量检查 output[] 中是否包含注入工具的 function_call
              ├── 无注入工具调用 → 直接透传 raw response bytes
              └── 包含注入工具调用 →
                  ├── 从 function_call item 中提取 call_id, name, arguments
                  ├── 执行注入的 MCP 工具
                  ├── 构造新请求：将上一轮 output items 追加到 input[]，
                  │   再添加 function_call_output items（包含 call_id 和 output）
                  ├── 同时回传 reasoning items（推理模型必需）
                  ├── 从 Extra 中移除 previous_response_id（中间轮次不可引用服务端状态）
                  ├── 循环调用 LLM（最多 10 轮）
                  └── 最终响应中过滤注入工具的 function_call items
```

#### 透传原则（核心要求）

Responses API 字段众多且持续扩展，网关必须严格遵守透传原则：

1. **请求透传**：仅解析网关需要操作的字段（`input`、`tools`、`model`、`stream`），其余所有字段（`instructions`、`store`、`temperature`、`top_p`、`max_output_tokens`、`service_tier`、`reasoning`、`text`、`metadata`、`truncation` 等）原样透传到上游。例外：`previous_response_id` 在首次请求时透传，在工具循环的中间轮次中必须移除（见设计决策 10）
2. **响应透传**：仅解析 `output[]` 中的 `function_call` items 用于判断是否需要工具执行，其余 items（`message`、`reasoning`、`file_search_call`、`web_search_call`、`code_interpreter_call` 等）原样返回给客户端
3. **未知 item 类型透传**：对 `input[]` 和 `output[]` 中未识别的 item type，使用 `json.RawMessage` 保持原样，不解析也不丢弃
4. **function_call 内部字段透传**：`function_call` item 中除 `call_id`、`name`、`arguments` 外的字段（如 `id`、`status`）保持透传

#### Reasoning Items 处理

当上游是推理模型（如 o3、o4-mini、gpt-5）时，响应 `output[]` 中可能包含 `reasoning` items。在工具调用循环中，这些 reasoning items 必须与 function_call items 一起回传到下一轮 input 中，否则模型会报错或性能下降。处理方式：

- 将上一轮 `output[]` 中的所有 items（包括 reasoning items）追加到新请求的 `input[]`
- 仅追加 `function_call_output` items 对应网关注入工具的执行结果
- **混合工具调用**：与 Chat Completions 相同策略——若 output 中同时包含客户端工具和注入工具的 function_call，网关执行注入工具后中断循环，构造响应返回给客户端，仅保留客户端工具的 function_call items

#### 与 Chat Completions 的复用

`responses.go` 与 `chat.go` 共享以下逻辑（通过 `gateway/convert.go` 和 `toolexec/tool_exec.go` 复用）：

- MCP 工具执行逻辑（`internal/toolexec/tool_exec.go`）
- 工具名称映射（`<mcp_name>__<tool_name>`）和注入工具集合管理
- provider 选择和 fallback 逻辑（`gateway/selector.go`）
- 循环保护和超时控制

差异点由各自的 handler 独立处理：

- 请求/响应 JSON 的序列化和反序列化格式
- 工具定义注入格式（Chat Completions 有 `function` wrapper，Responses API 更扁平）
- 工具调用结果的回传格式（Message vs Item）
- 流式事件解析格式

### 4.6. Anthropic Messages API（透明代理，`handleProxy`）

`POST /*/v1/messages` 不在显式注册路由中，由 `router.NotFound` fallback 匹配路由前缀后调用 `handleProxy()` 透明转发。

**已具备的能力**（与 chat/responses 相同）：

- Provider 选择与 Failover
- 认证重试（401 时重新加载凭据）
- 请求头安全转发（清洗 hop-by-hop/客户端认证头，重建 `X-Forwarded-*`，再注入 provider 认证）
- 请求日志与 Token 指标
- 流式响应的 SSE 日志组装（`anthropic.AssembleStream`）

**缺失的能力**（因不解析请求体结构）：

- MCP 工具注入与执行
- System Prompt 注入

如需上述功能，应通过 `chat/completions` 端点以 OpenAI 格式发起请求，网关自动将其转换为 Anthropic Messages API 格式转发上游。

### 5. SSE 流式响应处理 (`pkg/protocol/sse.go`, `pkg/protocol/openai/stream.go`, `pkg/protocol/anthropic/stream.go`)

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

#### OpenAI Responses API 流式格式

Responses API 使用语义化事件名，格式与 Chat Completions 的 `data: {JSON}` 完全不同：

```
event: response.output_item.added
data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_xxx","name":"func_name","arguments":"","status":"in_progress"}}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"item-abc","output_index":0,"delta":"{\"ar","sequence_number":1}

event: response.function_call_arguments.delta
data: {"type":"response.function_call_arguments.delta","item_id":"item-abc","output_index":0,"delta":"g\":1}","sequence_number":2}

event: response.function_call_arguments.done
data: {"type":"response.function_call_arguments.done","item_id":"item-abc","output_index":0,"arguments":"{\"arg\":1}","sequence_number":3}

event: response.output_item.done
data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","call_id":"call_xxx","name":"func_name","arguments":"{\"arg\":1}","status":"completed"}}

event: response.completed
data: {"type":"response.completed","response":{...完整 response 对象...}}
```

关键点：

- `response.output_item.added` 事件携带 function_call 的 `name` 和 `call_id`
- `response.function_call_arguments.delta` 分片传输 arguments，通过 `item_id` 和 `output_index` 关联
- `response.function_call_arguments.done` 包含完整 arguments
- `response.completed` 包含完整的 response 对象，可直接从中提取所有 output items
- 文本内容通过 `response.output_text.delta` 事件流式传输
- 每个事件有 `sequence_number` 保证顺序

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

#### Responses API 流式的简化

相比 Chat Completions 流式处理，Responses API 流式有一个重要简化点：`response.completed` 事件包含完整的 response 对象，其中 `output[]` 包含所有已完成的 items。因此：

- 网关无需在流过程中逐 chunk 拼接 function_call arguments
- 只需缓冲所有 SSE 事件，等到 `response.completed` 事件到达时，直接从其 `response.output[]` 中提取完整的 `function_call` items
- 如果包含注入工具的 function_call，执行工具并构造新请求
- 最终一轮的 SSE 流中，需要过滤掉注入工具相关的 `response.output_item.added`、`response.function_call_arguments.delta/done`、`response.output_item.done` 事件，并修改 `response.completed` 事件中的 output 数组

### 5.5. 协议适配 (`internal/gateway/adapter.go`)

职责：为三种流式格式（OpenAI Chat Completions、OpenAI Responses API、Anthropic）提供统一的解析接口。流式解析器的具体实现分别位于 `pkg/protocol/openai/stream.go` 和 `pkg/protocol/anthropic/stream.go`。

```go
// StreamParser 接口定义在 pkg/protocol/sse.go 中
type StreamParser interface {
    Parse(events []Event, injectedTools []string) (toolCalls []ToolCallInfo, hasInjectedToolCall bool, err error)
    Filter(events []Event, injectedTools []string) []Event
}
```

`adapter.go` 中的 `newStreamParser(protocol, isResponses)` 工厂函数根据协议和 API 类型选择对应的 parser 实现：

- `openai.ChatStreamParser`：从 `delta.tool_calls[index].function.arguments` 拼接，以 `finish_reason: "tool_calls"` 判断结束
- `openai.ResponsesStreamParser`：从 `response.completed` 事件中直接提取完整 `function_call` items
- `anthropic.StreamParser`：从 `content_block_start` + `input_json_delta` 拼接，以 `stop_reason: "tool_use"` 判断结束

SSE 事件解析和 `ToolCallInfo` 统一类型定义在 `pkg/protocol/sse.go` 中，所有协议的解析器共享同一类型。

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

### 7. 工具注入 (`internal/gateway/tool_inject.go`)

职责：将 MCP 工具定义转换为 LLM API 的工具格式，注入到请求中。同时支持 Chat Completions 和 Responses API 两种格式。

```go
// InjectChatTools 将 MCP 工具定义注入到 chat completion 请求的 tools 字段中
func InjectChatTools(req *openai.ChatCompletionRequest, tools []mcp.Tool) []string

// InjectResponsesTools 将 MCP 工具定义注入到 responses 请求的 tools 字段中
func InjectResponsesTools(req *openai.ResponsesRequest, tools []mcp.Tool) []string

// 返回值：注入的工具名称列表，用于后续区分
```

MCP Tool → OpenAI Chat Completions Tool 映射：

```
MCP Tool.name        → tools[].function.name
MCP Tool.description → tools[].function.description
MCP Tool.inputSchema → tools[].function.parameters
（外层包裹 {"type":"function", "function": {...}}）
```

MCP Tool → OpenAI Responses API Tool 映射：

```
MCP Tool.name        → tools[].name
MCP Tool.description → tools[].description
MCP Tool.inputSchema → tools[].parameters
（扁平格式 {"type":"function", "name":..., "description":..., "parameters":...}）
```

### 8. 工具执行 (`internal/toolexec/tool_exec.go`)

职责：接收统一的 `ToolCallInfo`，对任意 tool call 触发全局 hook；对注入 MCP 工具执行调用并返回结果。工具执行逻辑本身与 API 格式无关，上层（chat.go / responses.go）负责将各自格式的 tool call 提取为统一类型传入。

```go
// ToolCallInfo 定义在 pkg/protocol/sse.go 中，是所有协议共享的统一类型
type ToolCallInfo struct {
    ID        string // tool_call.id (Chat Completions) or call_id (Responses API)
    Name      string // function name
    Arguments string // function arguments JSON
}

// Execute 对任意 tool_call 执行 hook，并返回注入 MCP 工具的执行结果
func Execute(ctx context.Context, calls []sse.ToolCallInfo, injectedTools []string,
    mcpClients map[string]*mcp.Client, mcpCfgs map[string]*config.MCPConfig,
    toolHooks []*config.HookRuleConfig, gatewayAddr string) ([]ToolResult, error)

// ToolResult contains the call ID and execution output
type ToolResult struct {
    CallID string
    Output string
    IsError bool
}
```

上层 handler 负责将 `ToolResult` 转换为各自 API 格式：

- `chat.go`：转换为 `role:"tool"` 的 Message
- `responses.go`：转换为 `type:"function_call_output"` 的 Item
- 非注入工具（客户端自行执行）同样会触发 hook；若 pre hook 返回拒绝，当前版本仅记录审计日志，不会由网关代为拦截客户端执行

### 9. OpenAI 类型定义 (`pkg/protocol/openai/types.go`)

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

// ChatCompletionResponse represents the response from POST /v1/chat/completions.
type ChatCompletionResponse struct {
    Choices []Choice                   `json:"choices"`
    Extra   map[string]json.RawMessage // id, model, usage, etc.
}

type Choice struct {
    Message      Message `json:"message"`
    FinishReason string  `json:"finish_reason,omitempty"`
    Extra        map[string]json.RawMessage // index, logprobs, etc.
}
```

注意：为了最大兼容性，对请求/响应 JSON 的未知字段应透传而非丢弃。使用自定义 `MarshalJSON`/`UnmarshalJSON` 实现。

### 9.1. OpenAI SSE 流式解析 (`pkg/protocol/openai/stream.go`)

实现 `sse.StreamParser` 接口的两个解析器：

- `ChatStreamParser` — 从 Chat Completions 流式 delta 中按 index 拼接 tool_calls arguments，以 `finish_reason: "tool_calls"` 判断结束
- `ResponsesStreamParser` — 从 `response.completed` 事件中提取完整 function_call items；Filter 方法过滤注入工具相关的 output_item.added/done 事件并修改 response.completed 中的 output 数组
- `FilterResponsesOutput()` — 从 output 数组中移除注入工具的 function_call items
- `ExtractCompletedResponse()` — 从 SSE 事件中找到 response.completed 并提取完整 response

### 9.2. Anthropic 协议适配 (`pkg/protocol/anthropic/`)

**anthropic.go** — OpenAI ↔ Anthropic Messages API 双向格式转换：

- `MarshalRequest()` — OpenAI → Anthropic：提取 system 到顶层、tool_calls → tool_use blocks、tools 格式转换、max_tokens 默认值
- `UnmarshalResponse()` — Anthropic → OpenAI：content blocks → message、stop_reason 映射
- `convertMessages()` — 消息格式转换，支持连续 tool messages 合并

**stream.go** — 实现 `sse.StreamParser` 接口：

- `StreamParser.Parse()` — 从 content_block_start/delta 事件按 index 拼接 tool_use arguments
- `StreamParser.Filter()` — 按 index 过滤注入工具相关的 content_block 事件

### 9.3. SSE 协议基础 (`pkg/protocol/sse.go`)

定义协议无关的 SSE 解析和统一类型：

- `Event` — SSE 事件结构（EventType, Data, Raw）
- `ParseEvents()` — 解析 raw SSE bytes 为结构化事件
- `ReplayEvents()` — 将事件转回 raw bytes 用于客户端重放
- `ToolCallInfo` — 统一的工具调用信息类型（ID, Name, Arguments string），被 `tool/executor.go` 和所有流式解析器共享
- `StreamParser` — 流式解析器接口（Parse + Filter）

### 9.5. OpenAI Responses API 类型定义 (`pkg/protocol/openai/responses.go`)

定义 OpenAI Responses API 的请求/响应结构体。同样遵循最小解析+最大透传原则。

```go
// ResponsesRequest represents a POST /v1/responses request.
// Only fields the gateway needs to inspect/modify are strongly typed;
// everything else is preserved via Extra.
type ResponsesRequest struct {
    Model  string            `json:"model"`
    Input  json.RawMessage   `json:"input"`             // string 或 []Item，延迟解析
    Tools  []json.RawMessage `json:"tools,omitempty"`   // 工具定义，保持 RawMessage 以透传非 function 类工具
    Stream bool              `json:"stream,omitempty"`
    Extra  map[string]json.RawMessage                   // instructions, previous_response_id, store, temperature, etc.
}

// ResponsesResponse represents the response from POST /v1/responses.
type ResponsesResponse struct {
    ID     string            `json:"id"`
    Output []json.RawMessage `json:"output"`            // output items，保持 RawMessage
    Extra  map[string]json.RawMessage                   // model, usage, metadata, etc.
}

// FunctionCallItem represents a function_call output item.
// Parsed from output[] when type == "function_call".
type FunctionCallItem struct {
    Type      string          `json:"type"`       // always "function_call"
    CallID    string          `json:"call_id"`
    Name      string          `json:"name"`
    Arguments string          `json:"arguments"`
    ID        string          `json:"id,omitempty"`
    Status    string          `json:"status,omitempty"`
    Extra     map[string]json.RawMessage              // future-proof
}

// FunctionCallOutputItem represents a function_call_output input item
// constructed by the gateway after executing injected tools.
type FunctionCallOutputItem struct {
    Type   string `json:"type"`    // always "function_call_output"
    CallID string `json:"call_id"`
    Output string `json:"output"`  // tool execution result (JSON string or plain text)
}

// ResponsesFunctionTool represents a function tool definition
// in the Responses API flat format.
type ResponsesFunctionTool struct {
    Type        string          `json:"type"`        // "function"
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    Parameters  json.RawMessage `json:"parameters,omitempty"`
    Strict      *bool           `json:"strict,omitempty"`
}
```

**Input 字段的处理策略**：

`input` 字段可以是字符串或 Item 数组，因此类型为 `json.RawMessage` 延迟解析。处理时先判断首字节：

- `"` 开头 → 字符串输入，首次请求直接透传；工具循环中间轮次需转为 `[]Item` 形式（包装为 `{"type":"message","role":"user","content":"..."}` item），以便追加 output items
- `[` 开头 → Item 数组，解析为 `[]json.RawMessage`，每个 item 仅在需要时按需深度解析
- 解析方式：先 unmarshal 为 `struct{ Type string }` 判断类型，再决定是否深度解析
- 未识别的 item type 原样保留

**Tools 数组的处理策略**：

`tools` 中可能包含非 function 类型的工具（如 `web_search`、`file_search`、`code_interpreter`、`mcp` 等内建工具），网关只追加 `type:"function"` 的 MCP 工具定义，不修改也不删除客户端传入的任何工具。

### 10.1. 服务安装 (`internal/install/service.go`)

安装 warden 为 systemd 服务：

- 复制二进制到 `/usr/local/bin/warden`
- 创建 systemd service 文件（`ExecStart={path} -p -c /etc/warden.yaml`）
- 使用 `config.ExampleConfig` 写入默认配置到 `/etc/warden.yaml`
- 支持首次安装和更新两种流程

### 10.2. 请求日志 (`internal/reqlog/`)

职责：记录每次请求/响应往返的结构化数据，支持多后端输出，并向 SSE 订阅者广播。

**核心类型**（`reqlog.go`）：

```go
// Logger is the logging backend interface.
type Logger interface {
    Log(Record)
    Close() error
}

// Record holds structured data for one request/response round-trip.
type Record struct {
    Timestamp   time.Time
    RequestID   string
    Route       string
    Endpoint    string
    Model       string
    Stream      bool
    Provider    string
    UserAgent   string
    DurationMs  int64
    Error       string
    Fingerprint string          // session grouping key, see BuildFingerprint
    Request     json.RawMessage
    Response    json.RawMessage
    Steps       []Step          // intermediate tool call iterations
}

// Step records one intermediate round-trip during tool call execution.
type Step struct {
    Iteration   int
    ToolCalls   []ToolCallEntry
    ToolResults []ToolResultEntry
    LLMRequest  json.RawMessage
    LLMResponse json.RawMessage
}
```

**导出函数**：

- `GenerateID() string` — 生成 8 字符十六进制请求 ID
- `BuildFingerprint(rawBody json.RawMessage) string` — 从请求体构建会话指纹（见下）
- `(r *Record) Sanitize()` — 确保所有 `json.RawMessage` 字段包含有效 JSON

**日志后端**：

| 文件        | 类型          | 说明                                                                                          |
| ----------- | ------------- | --------------------------------------------------------------------------------------------- |
| `file.go`   | `FileLogger`  | 每条记录写入独立 JSON 文件，文件名格式：`{route}_{时间戳}_{id}.json`                          |
| `http.go`   | `HTTPLogger`  | 异步推送到 HTTP 端点，缓冲队列 256 条，支持 Go 模板渲染请求体（sprig 函数可用），内置重试     |
| `logger.go` | `multiLogger` | 扇出到多个后端；`newLogger(cfg)` 工厂函数按配置构建，返回 nil（无目标）/ 单后端 / multiLogger |

**会话指纹（Fingerprint）**：

`BuildFingerprint` 用 gjson 轻量解析请求体，从消息内容构建紧凑的会话标识字符串，用于在日志中识别连续对话。

- 格式：`{sys_hash}{fsm}`
    - `sys_hash`：所有 system prompt 文本的 FNV-32a hash，6 位十六进制
    - `fsm`：各轮用户/助手输入的 hash 链，宽度递减（6→5→4→3→2→2→…）
- 两条记录属于同一会话：model 相同、sys_hash 相同、且较早记录的 fsm 是较新记录 fsm 的严格前缀
- 跳过 `x-anthropic-billing-header` 行（billing 内容变化不影响指纹）
- 跳过 `thinking` 块（推理内容动态变化不影响指纹）
- 同时支持 Chat Completions（`messages[]`）和 Responses API（`input[]`）

**广播器**（`broadcast.go`）：

- `Broadcaster.Publish(r Record)` — 写入环形缓冲（50 条）并 non-blocking fan-out 给所有订阅者
- `Subscribe() / Unsubscribe(ch)` — SSE handler 用于实时日志推送
- `Recent() []Record` — 按时间顺序返回最近 50 条记录（新连接时回放历史）

### 11. Web 管理面板 (`internal/gateway/admin.go`, `web/admin/`)

当 `admin_password` 配置不为空时，Gateway 在 `/_admin/` 路径下注册 Web 管理面板。

**后端 API**（`internal/gateway/admin.go`）：

| 方法 | 路径                                    | 说明                                                              |
| ---- | --------------------------------------- | ----------------------------------------------------------------- |
| GET  | `/_admin/`                              | embed 的前端 SPA                                                  |
| GET  | `/_admin/*filepath`                     | 前端静态资源                                                      |
| GET  | `/_admin/api/status`                    | provider 状态（含请求统计）+ route + MCP 信息                     |
| GET  | `/_admin/api/config`                    | 当前配置（api_key 脱敏为 `"***"`）                                |
| PUT  | `/_admin/api/config`                    | 更新配置（validate + 写入文件 + 还原 `***` 值）                   |
| POST | `/_admin/api/restart`                   | 发送 SIGTERM 触发进程优雅退出（由外部进程管理器重启）             |
| POST | `/_admin/api/providers/health`          | Provider 探活（调用 fetchModels 测试连通性）                      |
| GET  | `/_admin/api/providers/detail?name=xxx` | Provider 详情（配置 + 统计 + 模型列表）                           |
| POST | `/_admin/api/providers/suppress`        | 手动抑制/解除抑制 Provider（运行时生效）                          |
| POST | `/_admin/api/config/validate`           | 配置验证（不保存）                                                |
| GET  | `/_admin/api/routes/detail?prefix=/xxx` | Route 详情（关联 providers 统计 + MCP 工具状态 + system prompts） |
| GET  | `/_admin/api/mcp/detail?name=xxx`       | MCP 详情（命令、工具列表含 disabled 状态、路由引用、连接状态）    |
| POST | `/_admin/api/mcp/tool-call`             | MCP 工具调用（指定 mcp、tool、arguments，返回结果和耗时）         |
| POST | `/_admin/api/mcp/tool-toggle`           | 运行时 enable/disable 单个工具（内存生效，持久化需保存配置）      |
| GET  | `/_admin/api/logs/stream`               | SSE 实时日志推送                                                  |
| GET  | `/_admin/api/metrics/stream`            | SSE 仪表盘指标推送（聚合快照 + 滚动时序点）                       |

- 认证：HTTP Basic Auth，用户名 `admin`，密码 `cfg.AdminPassword`
- 配置更新：写入前检查文件 hash 防止并发冲突
- 静态资源压缩：优先读取 `*.br` 预压缩文件，并按 `Accept-Encoding` 严格协商；不支持 `br` 的客户端回退为服务端即时解压后返回

**实时日志广播器**（`internal/reqlog/broadcast.go`）：

- `Broadcaster` — 内存广播器，环形缓冲最近 **50** 条完整 `Record`，SSE 推送完整记录，non-blocking fan-out 到所有订阅者
- 所有请求处理通过 `Gateway.recordAndBroadcast()` 统一发布到文件日志和广播器
- Dashboard 指标流通过 `dashboardMetricsStore` 在网关内按 **2s** 周期采样 Prometheus 累计指标与输出速率 gauge，维护最近 **180** 个点（约 6 分钟）的滚动时序；输出速率时序同时保留总 TPS 和按 provider 聚合的 TPS；同时为路由页保留按 route 聚合的请求速率、失败率、输出速率时序；当进程重启或计数器回退时清空历史并重建 baseline，避免把回退后的累计值误判为流量尖峰

**Selector 监控**（`internal/gateway/selector.go`）：

- `ProviderStatus` — 暴露 provider 运行时健康状态（含请求统计：total_requests, success_count, failure_count, avg_latency_ms）
- `ProviderStatuses()` — 返回所有 provider 的健康快照
- `ProviderDetail(name)` — 返回单个 provider 的健康状态
- `ProviderModels(name)` — 返回单个 provider 的模型列表

**前端**（`web/admin/`）：

- Vue 3 + Vite，纯 CSS（无 UI 框架），构建产物 embed 到 Go 二进制
- 导航栏品牌图标使用 `web/admin/src/assets/thresh-horses-icon.svg`（主题意向：链钩牵引马群前冲）；浏览器 favicon 使用 `web/admin/public/favicon.svg` 并在构建时压缩为 brotli 资源
- Dashboard：Provider 卡片（健康色标、请求统计、延迟、Ping 按钮），点击进入详情页；路由列表（Prefix 可点击进入 Route 详情页）、MCP 状态卡片（点击进入详情页）；监控卡片优先展示客户端代理运营指标（请求用量、token 用量、输出速率、失败率、failover/stream error 压力、路由错误热点），并保留 TTFT/throughput 慢组合用于性能定位；用量概览、输出速率与错误压力改为基于 **uPlot** 的实时折线图，前端直接消费后端下发的滚动时序点，不再本地重算 Prometheus 累计计数器增量；其中输出速率图仅保留单张折线图，并按 provider 维度展示多条曲线；三张图统一使用同一时间窗口并通过 uPlot cursor sync 同步悬浮时间轴；通过 `/_admin/api/metrics/stream` 实时刷新
- Routes：路由列表上方展示基于同一 SSE 指标流的 route 维度多折线图，请求速率、失败率、输出速率都使用后端直接下发的滚动时序点，按最近活跃 route 自动挑选展示曲线，并与 Dashboard 一样使用 uPlot 渲染
- ProviderDetail：Provider 基本信息、运行时状态、模型别名、可用模型列表、Health Check 按钮
- RouteDetail：Route 基本信息、system prompts、关联 providers 统计表格、MCP 工具状态表格、请求发送面板（支持 chat/completions 和 responses 端点、stream 开关、JSON 编辑、响应展示）
- McpDetail：MCP 基本信息（命令、SSH、连接状态）、引用此 MCP 的路由列表、工具列表（点击进入工具详情页）
- McpToolDetail：工具详情（名称、描述、input schema）、enabled/disabled toggle 开关（运行时生效）、JSON 参数输入、调用按钮、结果展示（状态 + 耗时 + 输出）
- ToolHooks：全局 hook 规则管理（增删规则、match 通配符、exec/ai/http hook 完整字段配置、Save & Apply），并通过 `/_admin/api/tool-hooks/suggestions` 基于最近日志里的 OpenAI Chat / Responses / Anthropic `tool call` 聚合候选 match、route、model；建议卡片按 route 直接生成/填充 AI 规则，并支持一键补全 Exec/HTTP 规则骨架。AI/Exec/HTTP 建议按钮统一复用同一套“新增或填充已有规则”的判定逻辑；页面还提供可折叠的参数快速上手说明、可折叠的日志建议区块、MCP/工具拆分展示，以及偏向命令执行与隐私保护的默认 AI 安全提示词。AI hook 的 `route`/`model` 字段基于当前配置生成下拉项，其中 `model` 选项按 route 绑定的 providers 和 `system_prompts` 键动态收敛
- Config：通用配置编辑器除 provider/route/mcp/webhook 外，也提供 `tool_hooks` 的可视化编辑，支持 `exec` / `ai` / `http` 三种规则字段
- Config：结构化分区编辑器（General / SSH / Providers / Routes / MCP），每个 map 条目可折叠，敏感字段（api_key、admin_password）使用 password input + Configured/Not set 徽章，支持 Add/Delete 条目，全局 Save + Validate + Restart Gateway
- Logs：SSE 实时日志表格，最多 500 条，自动滚动，可暂停；会话分组采用“指纹优先 + 回退哈希”策略：优先按 `model + sys_hash + FSM 严格前缀` 连续性聚合，无指纹时回退到“用户消息 hash + 10 分钟时间窗”启发式；前端在 `Logs.vue` 内对 request 解析、preview、user-hash、fingerprint、timestamp 使用 per-log WeakMap 缓存，并按 `(model, sys_hash)` 与 `lastUserHash` 建立候选链索引，减少重复解析与全量回扫开销；耗时展示使用动态单位格式化，短时保留 `ms`，长时自动切换到 `s` / `m` / `h`。

## 实现顺序

| 阶段     | 内容                                                                                                                                              | 依赖      |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | --------- |
| **P1**   | `config/`、`pkg/protocol/openai/types.go`、`pkg/protocol/openai/responses.go`、`pkg/protocol/sse.go`、`cmd/warden/main.go`、`Makefile`            | 无        |
| **P2**   | `internal/gateway/gateway.go`、`internal/gateway/http.go`、`internal/gateway/adapter.go`、`internal/gateway/middleware.go`、`internal/app/app.go` | P1        |
| **P3**   | `internal/mcp/client.go` — MCP client，工具发现和调用                                                                                             | P1        |
| **P4**   | `internal/gateway/tool_inject.go`、`internal/toolexec/tool_exec.go` — 注入和执行                                                                  | P3        |
| **P5**   | `internal/gateway/chat.go` — Chat Completions 处理（非流式 + 流式）                                                                               | P2 + P4   |
| **P5.5** | `internal/gateway/responses.go` — Responses API 处理（非流式 + 流式）                                                                             | P2 + P4   |
| **P6**   | `pkg/protocol/openai/stream.go`、`pkg/protocol/anthropic/` — 协议适配和流式解析器                                                                 | P5 + P5.5 |

## 第三方依赖说明

| 依赖                                  | 用途                                                                       |
| ------------------------------------- | -------------------------------------------------------------------------- |
| `github.com/julienschmidt/httprouter` | 轻量级 HTTP 路由，与 contatto 保持一致                                     |
| `github.com/sower-proxy/deferlog/v2`  | 函数退出日志，项目规范要求                                                 |
| `github.com/sower-proxy/feconf`       | 配置解析管理，项目规范要求                                                 |
| `github.com/lmittmann/tint`           | slog 彩色输出                                                              |
| `github.com/mark3labs/mcp-go`         | MCP 协议 Go SDK（避免重新实现 JSON-RPC + MCP 握手）                        |
| `github.com/tidwall/gjson`            | 轻量级只读 JSON 解析，用于路由层字段提取和指纹构建，避免完整结构体反序列化 |
| `github.com/go-resty/resty/v2`        | HTTP 客户端，用于 HTTPLogger 推送日志，内置重试支持                        |
| `github.com/Masterminds/sprig/v3`     | HTTPLogger 模板函数库，支持自定义日志体渲染                                |

## 关键设计决策

1. **Provider 分离配置**：将上游 LLM 的连接信息（URL、认证、超时、协议）独立为 `provider` 配置块，route 通过名称引用。好处：同一个 provider 可被多个 route 复用；同一个 route 可配置多个 provider 实现 fallback；认证信息集中管理。

2. **协议适配**：`provider.protocol` 字段决定认证头注入方式和 API 格式适配：
    - `openai`：`Authorization: Bearer <api_key>`
    - `anthropic`：`x-api-key: <api_key>` + `anthropic-version` header
    - `ollama`：无认证头
    - `qwen`：`Authorization: Bearer <access_token>`，从 `config_dir/oauth_creds.json` 读取 OAuth token（支持 SSH 远程读取），API 格式兼容 OpenAI
    - `copilot`：`Authorization: Bearer <copilot_token>`，从 `config_dir/hosts.json` 读取 GitHub OAuth token（支持 SSH 远程读取），通过 GitHub API 交换短期 Copilot token，API 格式兼容 OpenAI

3. **JSON 透传**：请求/响应中网关不关心的字段必须原样透传，不能丢弃。使用 `json.RawMessage` + 自定义序列化实现。

4. **流式处理策略**：
    - route 无注入工具时：直接 pipe 转发，零额外延迟
    - route 有注入工具时：统一缓冲完整 SSE 流后判断是否有注入工具调用
    - 中间轮次（执行注入工具后重新请求）：始终缓冲
    - 最终一轮（无注入工具调用）：直接 pipe 转发，恢复真正的流式体验
    - 需分别实现 OpenAI Chat Completions（`delta.tool_calls[index].function.arguments` 拼接，`finish_reason: "tool_calls"`）、OpenAI Responses API（`response.function_call_arguments.delta` 拼接，`response.completed` 事件）和 Anthropic（`content_block_start` + `input_json_delta`，`stop_reason: "tool_use"`）三种流式 chunk 解析器

5. **工具名称空间**：MCP server 可能有同名工具。使用 `<mcp_name>::<tool_name>` 作为内部唯一标识，注入到 LLM 时使用 `<mcp_name>__<tool_name>` 格式（因为 OpenAI function name 不支持 `::`）。

6. **并发安全**：`RouteConfig.enabledTools` 需要 `sync.RWMutex` 保护，因为 admin API 可以运行时修改。App 通过 `sync.RWMutex` 保护 gateway 指针，确保热重载期间的并发安全。

7. **错误恢复**：工具执行失败时，将错误信息作为 tool result 返回给 LLM，让 LLM 自行决定如何处理，而非中断整个请求。

8. **循环保护**：tool_call → execute → re-request 循环设置最大轮次（默认 10），防止无限循环。

9. **Responses API 透传优先**：Responses API 字段远比 Chat Completions 丰富（`instructions`、`previous_response_id`、`store`、`reasoning`、`metadata`、`truncation`、`service_tier` 等），且 OpenAI 持续新增字段。网关采用"白名单解析，黑盒透传"策略——仅解析 `input`、`tools`、`model`、`stream` 四个字段用于工具注入逻辑，其余所有请求字段和响应字段通过 `json.RawMessage` + 自定义序列化原样透传。这确保网关不会因为上游 API 新增字段而丢失数据或需要更新代码。

10. **Responses API 的 `previous_response_id` 处理**：当客户端使用 `previous_response_id` 进行多轮对话时，网关在工具执行循环的中间轮次不应设置 `previous_response_id`（因为中间轮次的"上一轮响应"是网关内部构造的，不存在于 OpenAI 服务端）。中间轮次改为将所有 output items 显式追加到 `input[]` 中。仅客户端首次请求中的 `previous_response_id` 会被透传。

11. **Responses API 流式优化**：Responses API 的 `response.completed` 事件包含完整的 response 对象（包括所有 output items），因此流式场景下无需像 Chat Completions 那样手动从 delta chunks 中拼接 tool_call arguments。网关可以直接从 `response.completed` 事件中提取完整的 function_call items，简化流式处理逻辑。但缓冲策略仍然必要——需要等到流结束才能判断是否包含注入工具的调用。

12. **统一 ToolCallInfo 类型**：`protocol.ToolCallInfo`（定义在 `pkg/protocol/sse.go`）是所有协议共享的工具调用信息类型，`Arguments` 字段为 `string` 类型。`internal/toolexec/tool_exec.go` 直接使用 `[]protocol.ToolCallInfo` 作为参数，消除冗余转换。`chat.go` 和 `responses.go` 各自负责从自有格式中提取 `protocol.ToolCallInfo`、将 `ToolResult` 转换回自有格式。MCP 工具执行逻辑完全复用，不因 API 格式差异而重复实现。

13. **混合工具调用中断策略**：当 LLM 在同一轮同时调用了客户端工具和网关注入工具时，网关无法为客户端工具提供 tool result（只有客户端知道如何执行自己的工具）。继续循环会导致 LLM 因缺少 tool result 而报错。解决方案：网关执行完注入工具后**中断循环**，将注入工具的执行结果通过追加到上下文中继续请求 LLM，但在最终返回的响应中只保留客户端工具的 tool_calls。此策略同时适用于 Chat Completions 和 Responses API。

14. **按需解析，最大透传**：网关在请求/响应路径上尽量避免不必要的 JSON 序列化/反序列化。具体策略：
    - **请求侧**：先 `ReadAll` 原始 body bytes，使用 `gjson.GetBytes()` 轻量提取路由所需字段（`model`、`stream`），用 `gjson.ValidBytes()` 做 JSON 有效性校验。无工具注入时，raw bytes 直接透传到上游（OpenAI/Ollama 零拷贝，Anthropic 需 `marshalProtocolRaw` 协议转换）。仅在有工具注入时才完整 Decode 为 struct。
    - **响应侧**：`forwardNonStreamRequest` / `forwardResponsesRequest` 同时返回 parsed struct 和 raw bytes。无注入工具调用时直接透传 raw response bytes 给客户端，避免 struct → JSON 的再序列化。`hasInjectedFunctionCalls` 使用 `gjson.GetBytes()` 仅提取 `type`/`name` 两个字段做判断；`extractFunctionCalls` 仅在确认类型匹配后才完整 Unmarshal。
    - **流式侧**：无工具注入时直接 `pipeRawStream`，完全跳过 SSE 缓冲和解析。
    - **指纹构建**：`BuildFingerprint` 使用 gjson 按需提取 `messages`/`input`/`system` 等字段，支持 Chat Completions 和 Responses API 两种协议格式，跳过 `x-anthropic-billing-header` 和 `thinking` 块以保证指纹稳定性。

15. **SSH 远程支持（Shell out to system ssh）**：MCP server 启动支持通过 SSH 在远程主机执行。方案选择 shell out 到系统 `ssh` 命令（而非引入 Go SSH 库），原因：自动继承 `~/.ssh/config`、SSH agent、ProxyJump、证书等用户配置；`exec.Command("ssh", host, command...)` 提供与本地 exec 完全一致的 stdin/stdout pipe 接口，MCP JSON-RPC 协议无需任何修改；不引入新依赖。SSH 配置通过 `[ssh.<name>]` 配置块集中管理，MCP 通过 `ssh = "<name>"` 引用。

16. **重启而非热重载**：配置保存后通过 `POST /_admin/api/restart` 发送 SIGTERM 触发进程优雅退出，由外部进程管理器（systemd 等）负责重启。避免了热重载方案中 feconf 重复注册 flag 的问题，同时简化了 App 结构（移除 `sync.RWMutex` 和 gateway 原子替换逻辑）。

17. **双轨超时策略**：流式请求和非流式请求使用不同的超时策略：
    - **流式请求**：使用固定的 30s 首-token 超时（`firstTokenTimeout`）。流式响应应该在几秒内返回首个 token，否则说明上游有问题。首 token 之后，body 读取无时间限制。
    - **非流式请求**：使用可配置的超时（`provider.timeout`，默认 120s）。非流式请求需要等待完整响应生成，推理模型可能需要较长时间。超时仅应用于等待响应头（`ResponseHeaderTimeout`），body 读取无时间限制。
    - 延迟统计记录首-token 时间，用于 provider 健康度评估和 failover 决策。

18. **OpenAI 双向协议转换**：当 provider 配置 `chat_to_responses: true` 时，客户端仍通过 `POST /chat/completions` 发请求，但网关将请求转换为 Responses API 格式发送到上游 `/responses`，并将响应转回 Chat 格式返回客户端。反向地，当 provider 配置 `responses_to_chat: true` 时，客户端通过 `POST /responses` 发请求，网关将其转换为 Chat Completions 格式发送到上游 `/chat/completions`，再把响应转回 Responses 格式返回客户端。转换逻辑位于 `pkg/protocol/openai/convert.go`，核心函数包括 `ChatRequestToResponsesRequest`、`ResponsesRequestToChatRequest`、`ResponsesResponseToChatResponse`、`ChatResponseToResponsesResponse`、`ResponsesSSEToChatSSE`、`ChatSSEToResponsesSSE`。入口分流分别位于 `handleChatCompletion` 和 `handleResponses`，工具调用处理分别复用 chat/responses 既有循环逻辑，避免重复实现。`responses_to_chat` 明确只支持 Chat 兼容子集；Responses 原生内建工具与 `previous_response_id` 不可桥接。
