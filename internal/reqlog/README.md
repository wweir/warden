# reqlog

请求/响应日志包。记录每次 LLM 请求往返的结构化数据，支持多后端输出，并向 SSE 订阅者实时广播。

## 文件

| 文件 | 职责 |
|------|------|
| `types.go` | 核心类型（`Record`、`Step`、`Logger` 接口） |
| `fingerprint.go` | `BuildFingerprint` 及其 JSON 提取、文本归一化、哈希辅助函数 |
| `record.go` | `GenerateID`、`Record.Sanitize`、JSON 包装辅助函数 |
| `file.go` | `FileLogger`：每条记录写入独立 JSON 文件 |
| `http.go` | `HTTPLogger`：异步推送日志到 HTTP 端点，支持模板渲染 |
| `broadcast.go` | `Broadcaster`：内存广播器，SSE 推送 + 环形缓冲 |
| `internal/gateway/logger.go` | `newLogger`/`multiLogger`：按配置构建多后端 Logger，`reqlog` 包本身只定义接口和后端实现 |

## 核心类型

### Logger 接口

```go
type Logger interface {
    Log(Record)
    Close() error
}
```

所有日志后端实现此接口。`gateway` 通过此接口写日志，不依赖具体实现。

### Record

一次请求/响应往返的完整记录：

```go
type Record struct {
    Timestamp   time.Time
    RequestID   string          // 8 字符十六进制，由 GenerateID() 生成
    Route       string          // 路由前缀，如 "/anthropic"
    Endpoint    string          // 端点，如 "chat/completions"
    Model       string
    Stream      bool
    Pending     bool            // 流式请求的中间态；最终记录会用同一 request_id 覆盖
    Provider    string
    UserAgent   string
    DurationMs  int64
    Error       string
    Fingerprint string          // 会话指纹，见 BuildFingerprint
    Request     json.RawMessage
    Response    json.RawMessage
    TokenUsage  *TokenUsage     // 归一化后的 token usage 观测结果
    Failovers   []Failover      // 同一请求内的上游切换轨迹
    Steps       []Step          // 工具调用中间轮次
}
```

### TokenUsage

请求级 token usage 观测结果：

```go
type TokenUsage struct {
    PromptTokens     int64
    CompletionTokens int64
    CacheTokens      int64
    TotalTokens      int64
    Source           string // reported_json / reported_sse / bridge_normalized
    Completeness     string // exact / partial / missing
}
```

### Failover

同一个客户端请求内每次上游切换都会追加一条轨迹：

```go
type Failover struct {
    FailedProvider      string
    FailedProviderModel string
    NextProvider        string
    NextProviderModel   string
    Error               string
}
```

这用于补齐 failover 成功场景的请求日志：最终请求虽然成功，但日志仍能看出中间失败的是谁、切到了谁、触发错误是什么。

### Step

工具调用循环中每一轮的记录：

```go
type Step struct {
    Iteration   int
    ToolCalls   []ToolCallEntry
    ToolResults []ToolResultEntry
    LLMRequest  json.RawMessage
    LLMResponse json.RawMessage
}
```

## 导出函数

### GenerateID

```go
func GenerateID() string
```

生成 8 字符十六进制随机 ID，用于标识单次请求。

### BuildFingerprint

```go
func BuildFingerprint(rawBody json.RawMessage) string
```

从请求体构建会话指纹，用于在日志中识别连续对话。使用 gjson 轻量解析，不做完整反序列化。

**格式**：`{sys_hash}{fsm_chain}`

- `sys_hash`：所有 system prompt 文本拼接后的 FNV-32a hash，6 位十六进制
- `fsm_chain`：每轮用户/助手输入的 hash，宽度依序递减（6→5→4→3→2→2→…），无分隔符

**会话连续性**：指纹本身只表达 system prompt 与对话步骤序列，不包含 model / route。管理端在日志页中会先用显式 `previous_response_id -> response.id` 续接；没有显式续接时，再把同 route 下且 `sys_hash` 相同、并满足“较早记录指纹是较新记录指纹严格前缀”的请求保守归并为同一会话。

**稳定性保证**：
- 过滤 `x-anthropic-billing-header:` 行（billing token 变化不影响指纹）
- 跳过 `thinking` 块（推理内容动态，不纳入指纹）
- 对 assistant 文本截取前 100 字符（避免长文本导致指纹过长）

支持 Chat Completions API（`messages[]`）和 Responses API（`input[]`）两种协议。

### Sanitize

```go
func (r *Record) Sanitize()
```

确保 `Record` 中所有 `json.RawMessage` 字段包含有效 JSON。无效内容会被包装为 JSON 字符串。写入日志前调用。

## 日志后端

### FileLogger

```go
func NewFileLogger(dir string) (*FileLogger, error)
```

将每条 `Record` 序列化为缩进 JSON，写入 `dir` 目录下的独立文件。

文件名格式：`{route}_{月日-时分秒.毫秒}_{request_id}.json`

例：`anthropic_0115-143022.123_a1b2c3d4.json`

写入失败仅打 warn 日志，不影响请求处理。

### HTTPLogger

```go
func NewHTTPLogger(cfg HTTPLoggerConfig) (*HTTPLogger, error)

type HTTPLoggerConfig struct {
    URL          string
    Method       string            // 默认 POST
    Headers      map[string]string
    BodyTemplate string            // Go 模板，变量 .Record；为空则直接 JSON 序列化
    Timeout      string            // 默认 5s
    Retry        int
}
```

异步推送日志到 HTTP 端点：

- 后台单 goroutine worker，通过 256 容量 channel 接收记录
- 队列满时静默丢弃（打 warn 日志）
- `BodyTemplate` 支持 [sprig](https://masterminds.github.io/sprig/) 函数
- `Close()` 取消 worker、终止 in-flight HTTP 请求，并丢弃未发送队列，避免 shutdown 被日志重试拖住

### multiLogger（内部）

`newLogger(cfg *config.LogConfig) Logger` 按配置构建 Logger：

- 无目标：返回 `nil`
- 单目标：直接返回该后端
- 多目标：返回 `multiLogger`，将 `Log()` 调用扇出到所有后端，`Close()` 返回第一个错误

## Broadcaster

```go
func NewBroadcaster() *Broadcaster

func (b *Broadcaster) Publish(r Record)
func (b *Broadcaster) Subscribe() chan Record
func (b *Broadcaster) Unsubscribe(ch chan Record)
func (b *Broadcaster) Recent() []Record
```

内存广播器，供 SSE 实时日志推送使用：

- **环形缓冲**：保留最近 50 条记录，新 SSE 连接建立时通过 `Recent()` 回放历史；相同 `request_id` 的新事件会覆盖旧记录，避免 pending/final 重复堆积
- **非阻塞 fan-out**：`Publish` 向所有订阅者 channel 发送，慢消费者丢弃该事件（不阻塞发布方）
- 每个订阅者 channel 缓冲 64 条
- `Unsubscribe` 关闭 channel，SSE handler 通过 range 感知断开
