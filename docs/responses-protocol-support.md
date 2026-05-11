# Responses 协议支持现状

> 更新日期：2026-05-11
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

`responses` route 同时接收 `/responses` 的无状态与有状态请求。网关在 `internal/gateway/api_responses.go` 中先读取请求体，然后按是否存在 `previous_response_id` 选择处理链路：

- **无状态请求(无 `previous_response_id`)** 走 inference handler：
  - 支持 failover（按 route model 候选列表内重试）
  - 解析 tool calls 并触发 route hooks
  - 当命中的 provider 启用 `responses_to_chat` 且 family=openai 时，自动桥接到上游 `/chat/completions`
  - 走 `internal/gateway/bridge` 的流式中继与 Chat ↔ Responses 状态机
- **有状态请求(带 `previous_response_id`)** 直接走透明转发：
  - 调用 `internal/gateway/proxy/Handler`，与命中 `notFoundHandler` 的非推理子路径走同一条代码路径
  - 仍然支持 route target 的模型重写（`upstreams[].model` 不同名时）
  - 仍然按 provider 注入鉴权头与 `X-Forwarded-*`
  - 仍然按 route api keys 做客户端鉴权
  - 仍然写请求日志、广播 SSE、统计 Prometheus 指标
  - **不**解析响应体中的 tool calls、**不**执行 route hooks、**不** failover

> 网关本身不维护任何 Responses 会话状态。`previous_response_id` 是否被上游正确接受，完全由上游 provider 是否持有对应 `response.id` 决定。

## 3. `responses_to_chat`

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

## 4. 管理端协同

- 管理端 `Chat` 页面在 `responses` route 下会把返回的 `response.id` 保存在浏览器 `localStorage` 的会话记录里，并在下一轮自动续传 `previous_response_id`；该状态仅存在于浏览器，不依赖网关后端
- 管理端 `Logs` 页面整合会话时，优先使用 Responses 请求中 `previous_response_id -> response.id` 的显式关联；没有显式关联时只在同 route 下退回 fingerprint 前缀做保守归并

## 5. 当前判断

可以明确说支持的：

- 原生 `/responses` 无状态请求(走 inference handler)
- 带 `previous_response_id` 的原生 `/responses` 请求(走透明转发)
- 管理端基于 `response.id` 的本地多轮串接

只能说部分支持的：

- `responses_to_chat` 下的无状态兼容；启用后该 provider 仍可参与 `responses` route，但只承接无状态请求

不能说支持的：

- `responses_to_chat` 下的 `previous_response_id`(请求会直接进入透明转发链路,不再经过桥接);如果上游是 chat-only 不暴露 `/responses`,透明转发会直接返回上游的 4xx,网关不会自动降级到 chat
- 任何需要网关自己维护 Responses 会话状态的能力
- 依赖网关多 upstream failover 维持同一条 Responses 多轮会话的能力
