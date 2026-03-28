# Responses API 有状态 / 无状态支持现状

> 更新日期：2026-03-19

本文只描述当前代码事实。

## 1. Route 结构前提

当前不再允许 model-level `protocols`。

Responses 相关 route 必须直接锁定唯一协议：

- `route.protocol: responses_stateless`
- `route.protocol: responses_stateful`

模型结构：

- `route.exact_models.<name>.upstreams`
- `route.wildcard_models.<pattern>.providers`

## 2. 语义

- `responses_stateless`
  - 只接受无状态 `/responses`
  - 明确拒绝 `previous_response_id`
- `responses_stateful`
  - 同时接受无状态和有状态 `/responses`
  - `exact_models` 只允许单 upstream
  - `wildcard_models` 只允许单 provider
  - 有状态请求禁用 failover

## 3. 原生 `/responses`

原生路径对应：

- `internal/gateway/api_responses.go`
- `pkg/protocol/openai/prompt.go`

事实：

- 无状态请求完整支持
- 有状态请求本质上是透传 `previous_response_id`
- warden 不维护本地 Responses 会话状态
- 状态是否成立取决于上游 provider 是否持有对应 `response.id`

因此真实结论是：

- 单 provider 或显式固定 `X-Provider` 时，`responses_stateful` 可用
- 多 provider 场景下不会对有状态请求做 failover
- 管理端 `Chat` 页面如果命中 `responses_stateful` route，会把上一轮 `response.id` 保存在浏览器本地会话中，并在下一轮自动续传 `previous_response_id`

## 4. `responses_to_chat`

当 provider 开启 `responses_to_chat` 时：

- 只允许无状态 `responses`
- 有状态 `previous_response_id` 明确不支持
- 顶层 `instructions` 会转换成首条 `developer` message
- 只接受受控的 Chat 兼容子集；不支持的 Responses 专有字段、非 `function` tools、未知 input item 直接返回 `400`
- 会显式兼容 `max_output_tokens -> max_completion_tokens`
- 会校验并规范化 Responses 风格 `tool_choice`；未知形状或引用未知 `function` tool 会直接返回 `400`
- `function_call_output.output` 可为字符串或任意 JSON；转换到 Chat `tool` message 时会规范化成字符串内容
- Chat -> Responses 回写会把 Chat `usage` 规范化为 Responses 风格的 `input_tokens` / `output_tokens`，并映射 `prompt_tokens_details/completion_tokens_details`
- Chat -> Responses 回写会把 Chat `finish_reason` 映射为 Responses `status` / `incomplete_details`
- 流式桥会补齐更接近原生 Responses 的生命周期事件和关联字段，而不是只输出最小 delta 集合；`response.output_item.done` 会附带最终 item 快照
- 如果上游以 `400` 明确拒绝 `developer` role，bridge 会在同一 provider 上自动回退为 `system` 重试一次
- 这是兼容桥接，不是完整 Responses 协议等价实现

## 5. 当前判断

可以明确说支持的：

- 原生 `/responses` 无状态请求
- 原生 `/responses` 的有状态透传
- `responses_stateful` route 对 stateless/stateful 请求的正确入口约束

只能说部分支持的：

- `responses_to_chat` 下的无状态兼容

不能说支持的：

- `responses_to_chat` 下的 `previous_response_id`
- 任何需要网关自己维护 Responses 会话状态的能力
