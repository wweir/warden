# protocol 包

LLM 协议处理的公共组件，提供协议无关的类型和接口，以及各协议的具体实现。

## 包结构

```
protocol/
├── sse.go              # 公共类型（Event、ToolCallInfo、StreamParser）和 SSE 解析/重放
├── openai/             # OpenAI 协议实现
│   ├── types.go        # Chat Completions API 请求/响应类型
│   ├── responses.go    # Responses API 请求/响应类型
│   ├── convert.go      # Responses -> Chat 请求转换；Chat -> Responses 响应/SSE 转换
│   ├── stream.go       # SSE 流式解析器（Chat + Responses）
│   └── prompt.go       # 系统提示词注入
└── anthropic/          # Anthropic 协议实现
    ├── anthropic.go    # OpenAI Chat ↔ Anthropic Messages 格式转换
    ├── chat_bridge.go  # Anthropic Messages -> OpenAI Chat 请求/响应/SSE 桥接
    ├── stream.go       # SSE 流式解析器
    └── auth.go         # 认证头设置
```

## 职责

- 定义 LLM 协议公共类型（`Event`、`ToolCallInfo`）
- 提供 SSE 流解析和重放功能
- 定义 `StreamParser` 接口供各协议实现
- 提供 OpenAI `responses_to_chat` 所需的无状态 `Responses -> Chat` 请求转换，以及 `Chat -> Responses` 响应/SSE 转换
- 提供 `anthropic_to_chat` 所需的受控 `Messages -> Chat` 请求转换，以及 `Chat -> Messages` 响应/SSE 转换
- `responses_to_chat` 转换器只接受受控的 stateless 子集；不支持的 Responses 专有字段、未知 input item、非 `function` tools 会直接报错
- `anthropic_to_chat` 转换器只接受文本 + `function` tools 子集；不支持的 content block、未知字段和无法线性映射的消息形状会直接报错
- 流式桥接使用增量状态机而不是整段字符串拼接；状态机会在流结束时校验 OpenAI `[DONE]` 或 Anthropic `message_stop`，缺失时把流视为不完整

## 主要类型

### Event

SSE 事件结构：

```go
type Event struct {
    EventType string   // 事件类型（如 "message"、"content_block_delta"）
    Data      string   // data 字段按 SSE 规则拼接后的结果
    HasData   bool     // 是否显式出现过 data 字段
    ID        string   // id 字段
    HasID     bool     // 是否显式出现过 id 字段
    Retry     *int     // retry 字段（仅在值合法时设置）
    Comments  []string // 注释行内容（不含前导冒号）
    Raw       string   // 原始 SSE 帧，重放时优先使用
}
```

解析器支持标准 SSE 帧字段：

- `event`
- `data`
- `id`
- `retry`
- `:` 注释行

并兼容 `LF`、`CRLF`、`CR` 三种行结束符。`ReplayEvents` 默认优先使用 `Raw` 重放原始帧内容；若事件是代码构造的，则按标准字段重新编码。

### ToolCallInfo

协议无关的工具调用信息：

```go
type ToolCallInfo struct {
    ID        string // tool call ID
    Name      string // function name
    Arguments string // function arguments JSON
}
```

### StreamParser 接口

流式响应解析器接口：

```go
type StreamParser interface {
    // Parse extracts tool call infos from SSE events
    Parse(events []Event) ([]ToolCallInfo, error)
}
```

## 函数

- `ParseEvents(raw []byte) []Event` - 解析原始 SSE 字节为事件列表
- `ReplayEvents(events []Event) []byte` - 将事件列表重新编码为 SSE 字节
