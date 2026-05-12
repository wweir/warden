# Responses 协议支持现状

> 更新日期：2026-05-12
>
> 状态：current

本文只描述当前代码事实。`responses_stateless` 与 `responses_stateful` 协议已合并为单一的 `responses` 协议；有状态语义改由请求体中的 `previous_response_id` 决定。

## 1. Route 结构前提

Responses 相关 route 锁定唯一协议：

- `route.protocol: responses`

模型结构：

- `route.exact_models.<name>.upstreams`
- `route.wildcard_models.<pattern>.providers`

旧字段 `responses_stateless` / `responses_stateful` 已被拒绝;升级前需把这些字符串在 `route.protocol` 与 `service_protocols` 中替换为 `responses`。

## 2. 请求分发

`responses` route 同时接收 `/responses` 的无状态与有状态请求。网关在 `internal/gateway/api_responses.go` 中先读取请求体，先 select 命中的 provider,再按是否存在 `previous_response_id` 与 provider 类型选择处理链路：

- **无状态请求(无 `previous_response_id`)**:
  - provider 启用 `responses_to_chat` 且 family=openai → `handleResponsesViaChat`,通过 chat_bridge 把请求转成 chat 发到上游 `/chat/completions`,再把响应转回 responses
  - provider 启用 `anthropic_to_responses` 且 family=anthropic → `handleResponsesViaMessages`,通过 chat_bridge 把请求转成 chat IR,upstream 层把 chat IR 序列化为 anthropic 上游 `/messages` 请求,再把响应转回 responses
  - 其它(openai family 原生 responses) → inference handler,支持 failover、tool hooks、Responses 流式中继
- **有状态请求(带 `previous_response_id`)** 走透明转发：
  - 命中 provider 必须不是 `anthropic_to_responses`(否则直接返回 `400`,因为 Anthropic 没有 conversation state 概念)
  - 调用 `internal/gateway/proxy/Handler`,与命中 `notFoundHandler` 的非推理子路径走同一条代码路径
  - 仍然支持 route target 的模型重写、provider 鉴权头注入、`X-Forwarded-*`、route api keys、请求日志、Prometheus 指标
  - **不**解析响应体中的 tool calls、**不**执行 route hooks、**不** failover

> 网关本身不维护任何 Responses 会话状态。`previous_response_id` 是否被上游正确接受,完全由上游 provider 是否持有对应 `response.id` 决定。

## 3. `responses_to_chat`(OpenAI family 上游)

`responses_to_chat` 只在无状态请求路径上生效。有状态请求会在更早的分发阶段进入透明转发，因此不再经过 Chat 兼容子集裁剪。

无状态请求下，`responses_to_chat` 仍然遵守以下约束：

- 顶层 `instructions` 会转换成首条 `developer` message；若上游以 `400` 明确拒绝 `developer` role，bridge 在同一 provider 上自动降级重试为 `system`
- 只接受受控的 Chat 兼容子集；不支持的 Responses 专有字段、非 `function` tools、未知 input item 直接返回 `400`
- 会显式兼容 `max_output_tokens -> max_completion_tokens`
- 会校验并规范化 Responses 风格 `tool_choice`；未知形状或引用未知 `function` tool 直接返回 `400`
- `function_call_output.output` 可为字符串或任意 JSON；转换到 Chat `tool` message 时规范化成字符串内容
- Chat -> Responses 回写会把 Chat `usage` 规范化为 Responses 风格的 `input_tokens` / `output_tokens`，并映射 `prompt_tokens_details/completion_tokens_details`
- Chat -> Responses 回写会把 Chat `finish_reason` 映射为 Responses `status` / `incomplete_details`
- 流式桥会补齐更接近原生 Responses 的生命周期事件和关联字段；`response.output_item.done` 会附带最终 item 快照

## 4. `anthropic_to_responses`(Anthropic family 上游)

`anthropic_to_responses` 允许 `family: anthropic` 的 provider 承接对外的 Responses 协议入口。请求转换链路:

```
client /responses
  → ResponsesRequestToChatRequest          (responses → chat IR)
  → upstream/transport.MarshalProtocolRequest("anthropic", ...) (chat IR → anthropic /messages 上游请求)
  → upstream Anthropic API
  → upstream/transport.UnmarshalProtocolResponse("anthropic", ...) (anthropic 响应 → chat IR)
  → ChatResponseToResponsesResponse        (chat IR → responses)
  → client /responses
```

流式响应受限于 Anthropic 流转换器不支持 stateful streaming,实现为 **buffered relay**:

- gateway 读完上游整段 anthropic SSE,用 `ConvertStreamToOpenAI` 转成 chat SSE,再走 `StreamChatAsResponses` 输出 responses SSE
- 客户端首字节延迟 = 上游完整响应延迟,损失流式语义
- token usage 仍按 anthropic 原始 SSE 解析,确保不依赖中间格式

约束:

- 只支持**无状态** Responses 请求;带 `previous_response_id` 的请求在 dispatch 时被 `400` 拒绝(因为 Anthropic 没有 server-managed conversation state)
- 配置层要求 `family == "anthropic"`,与 `anthropic_to_chat` 要求 `family == "openai"` 对称
- Anthropic extended thinking blocks 当前不会作为 Responses `reasoning` items 暴露,这是已知 lossy 点
- 由于通过 chat IR 中转,使用受限于 chat 表达能力的 Responses 子集(与 `responses_to_chat` 相同)

## 5. 管理端协同

- 管理端 `Chat` 页面在 `responses` route 下会把返回的 `response.id` 保存在浏览器 `localStorage` 的会话记录里，并在下一轮自动续传 `previous_response_id`；该状态仅存在于浏览器，不依赖网关后端
- 管理端 `Logs` 页面整合会话时，优先使用 Responses 请求中 `previous_response_id -> response.id` 的显式关联；没有显式关联时只在同 route 下退回 fingerprint 前缀做保守归并

## 6. 当前判断

可以明确说支持的：

- 原生 `/responses` 无状态请求(走 inference handler)
- 带 `previous_response_id` 的原生 `/responses` 请求(走透明转发)
- `responses_to_chat` provider 上的无状态 Responses(走 chat 上游桥接)
- `anthropic_to_responses` provider 上的无状态 Responses(走 anthropic 上游桥接,流式 buffered)
- 管理端基于 `response.id` 的本地多轮串接

不能说支持的：

- `responses_to_chat` provider 上的 `previous_response_id`(请求会直接进入透明转发链路,不再经过桥接);如果上游是 chat-only 不暴露 `/responses`,透明转发会直接返回上游的 4xx,网关不会自动降级到 chat
- `anthropic_to_responses` provider 上的 `previous_response_id`(直接 `400`)
- 任何需要网关自己维护 Responses 会话状态的能力
- 依赖网关多 upstream failover 维持同一条 Responses 多轮会话的能力
