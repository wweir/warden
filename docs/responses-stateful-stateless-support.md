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
