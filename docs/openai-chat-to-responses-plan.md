> Note (2026-03-13): route 级 MCP 工具注入已移除；本文中与“注入/拦截执行注入工具”相关的段落仅保留为历史设计记录。

# OpenAI Provider: Chat 协议转 Responses 协议方案

## 1. 目标

在不改变客户端调用方式（仍调用 `POST /chat/completions`）的前提下，让 **openai 协议 provider** 可选地把上游请求改走 `POST /responses`，并保证：

1. 支持非流式请求；
2. 支持流式请求（SSE）；
3. 保持现有 failover、鉴权重试与协议转换链路可用。

---

## 2. 先把隐含假设挑明（否则会踩坑）

你的需求里有 3 个未明说但影响很大的假设：

1. **假设 A：Chat 和 Responses 字段完全等价**  
   不成立。`messages/tool_calls` 与 `input/output(function_call)` 结构不同，部分参数名字和语义不同（如 token 控制字段）。
2. **假设 B：流式事件可直接透传**  
   不成立。Responses 的 SSE 事件类型和 Chat chunk 结构不同，必须做事件级转换。
3. **假设 C：现有“原始透传优化”可继续复用**  
   大概率不成立。Chat->Responses 必须做请求/响应双向转换，至少在该模式下不能走纯 raw passthrough。

---

## 3. 当前实现差距（基于现有代码）

1. `internal/gateway/api_chat.go` 固定按 Chat 语义处理，上游 endpoint 固定走 `protocolEndpoint(..., false)`。
2. `internal/gateway/adapter.go` 仅区分 protocol + isResponses，没有“Chat 入口但上游用 Responses”的模式开关。
3. `pkg/protocol/openai` 缺少 Chat<->Responses 的通用转换器（请求、响应、SSE）。
4. Chat 流式工具拦截依赖 Chat chunk 解析；若上游改 Responses，解析器和回放都需适配。

---

## 4. 设计方案

### 4.1 配置开关（provider 级）

在 `config.ProviderConfig` 增加：

```go
ChatToResponses bool `json:"chat_to_responses" usage:"Route chat/completions to upstream /responses for openai protocol"`
```

校验规则：

1. 仅允许 `protocol == "openai"` 时开启；
2. 默认 `false`，保证现网零行为变化。

`config/warden.example.yaml` 增加示例：

```yaml
provider:
    openai:
        protocol: "openai"
        chat_to_responses: true
```

### 4.2 协议转换层（新增独立模块）

建议新增 `pkg/protocol/openai/chat_responses_convert.go`，提供 3 类函数：

1. `ChatRequestToResponsesRequest(chatReq) -> responsesReq`
2. `ResponsesResponseToChatResponse(resp) -> chatResp`
3. `ResponsesSSEToChatSSE(rawSSE) -> chatSSE`

核心映射规则：

1. `messages` -> `input`（`system` 统一映射为 `developer`）；
2. `role=tool` 消息 -> `function_call_output` item；
3. `assistant.tool_calls` -> `function_call` output item；
4. `tools[].function` -> responses flat `tools[]`；
5. `usage` 尽量映射回 Chat 结构（缺失则置零，不伪造）。

### 4.3 Chat 非流式流程改造

入口仍是 `handleChatCompletion`，但在选中 provider 后判断：

- 若 `protocol=openai && chat_to_responses=true`：
    1. Chat 请求先转 Responses 请求；
    2. 上游 endpoint 改为 `/responses`；
    3. 上游响应转回 Chat JSON 再返回客户端。

- 否则走现有流程（不动）。

重点：在该模式下禁用“无工具 raw passthrough 快路”，改为结构化转换路径。

### 4.4 Chat 流式流程改造

流程：

1. 上游请求发到 `/responses`（`stream=true`）；
2. 接收 Responses SSE；
3. 转换为 Chat SSE chunk（包含 `[DONE]`）后回给客户端；
4. 工具拦截仍走现有 `processStreamToolCalls`，但解析器改为可识别 Responses 事件。

建议实现：

1. `newStreamParser` 增加“chat 入站但 responses 上游”分支；
2. `convertStreamIfNeeded` 增加 Responses->Chat 的转换分支（仅对该模式生效）。

### 4.5 工具调用与 MCP 兼容

目标：不重写一套工具执行框架，只扩展转换层。

1. 继续复用 `toolexec.Execute` 与现有注入工具判定；
2. 把 Responses 的 `function_call` 统一提取为 `protocol.ToolCallInfo`；
3. 执行后的结果在下一轮请求里以 `function_call_output` 形式拼回（由转换层负责）。

### 4.6 错误与回退策略

1. 转换失败：返回 `502`（上游协议转换错误），日志记录 `provider + mode=chat_to_responses`；
2. 上游 4xx/5xx：沿用现有 `tryAuthRetry + tryFailover`；
3. 配置开关可随时关闭，立即回退为原生 Chat 上游路径。

---

## 5. 任务拆解（执行顺序）

### P1 配置与路由判定

1. 扩展 `config.ProviderConfig` 与 Validate；
2. 更新 `config/warden.example.yaml`；
3. 增加单测：合法/非法组合。

### P2 转换器实现（非流式先行）

1. 新增 Chat->Responses 请求转换；
2. 新增 Responses->Chat 响应转换；
3. 覆盖 message/tool/tool_call 的关键映射单测。

### P3 Chat 非流式接入

1. `api_chat.go` 接入新模式；
2. 保留旧模式零改动；
3. 补充集成测试（httptest fake upstream `/responses`）。

### P4 Chat 流式接入

1. 实现 Responses SSE -> Chat SSE 转换；
2. 接入流式工具拦截链路；
3. 补充流式集成测试（文本增量、tool_calls、finish_reason）。

### P5 文档与验收

1. 更新 `ARCHITECTURE.md`（新增 provider chat_to_responses 设计与时序）；
2. 如涉及复杂包，补充对应 `README.md`；
3. 跑 `go test`、`go vet`，验证回归。

---

## 6. 验收标准（DoD）

1. 客户端请求 `POST /chat/completions`，在 `chat_to_responses=true` 时能成功走上游 `/responses`；
2. 非流式响应格式仍是 Chat Completion JSON；
3. 流式响应格式仍是 Chat Completion SSE（含 `[DONE]`）；
4. 注入工具与客户端工具混合场景可正确拦截、过滤、继续对话；
5. 关闭开关后行为与当前版本一致；
6. 新增测试覆盖通过。

---

## 7. 主要风险与缓解

1. **字段语义不一致**：先做白名单映射，未支持字段显式报错，避免静默行为漂移；
2. **流式事件丢失信息**：保留原始事件到 debug 日志，便于对账；
3. **性能回退**：仅在 `chat_to_responses=true` 时启用结构化转换，默认路径不受影响。
