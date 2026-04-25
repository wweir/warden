# Embeddings 支持现状

> 更新日期：2026-04-22
>
> 状态：current

本文只描述当前代码事实。

## 1. 定位

`embeddings` 不是新的 `route.protocol`。

当前仍只有四个 route 协议：

- `chat`
- `responses_stateless`
- `responses_stateful`
- `anthropic`

`/embeddings` 是额外的 service protocol。`route.service_protocols` 留空时会按 `route.protocol` 推导并在有上游支持时暴露 embeddings；显式配置时必须包含 `embeddings` 才会暴露该入口，并且至少一个 route upstream/provider 必须支持 embeddings：

- `chat -> /chat/completions + /embeddings`
- `responses_stateless -> /responses + /embeddings`
- `responses_stateful -> /responses + /embeddings`
- `anthropic -> /messages + /embeddings`

## 2. 报文形状

所有 route 上的 `/embeddings` 都统一使用 OpenAI / Voyage 风格 JSON：

- 请求：`model` + `input`
- 响应：`object=list`、`data[].embedding`、`usage`

当前没有引入 Anthropic 专有 embeddings 报文。

因此 `route.protocol=anthropic` 的含义只是：

- 共享 Anthropic route 的鉴权、模型匹配、failover、日志和 metrics 体系
- 但 `/embeddings` 的请求/响应形状仍是 OpenAI-compatible JSON

## 3. Provider 能力边界

当前 provider 的 service protocol 能力不再直接等同于 adapter family。

默认能力：

- `openai`
  - `chat`
  - `responses_stateless`
  - `responses_stateful`
  - `embeddings`
  - 若开启 `anthropic_to_chat`，再额外支持 `anthropic`
- `anthropic`
  - `chat`
  - `anthropic`
- `copilot`
  - `chat`

关键结论：

- 原生 `anthropic` provider 不承接 `/embeddings`
- `route.protocol=anthropic` 只有在命中的模型最终落到 OpenAI-compatible provider 时，`/embeddings` 才能成功
- 如果 provider 显式配置了 `service_protocols`，则以该字段为准
- OpenAI-compatible 第三方上游（例如 Ollama）统一配置为 `openai`；如果只支持聊天接口，必须显式设置 `service_protocols: [chat]`，否则会被视为具备完整 OpenAI 默认能力

## 4. 运行时行为

- `/embeddings` 现在属于正式 inference endpoint
- 会走 route model 选路，而不是“未知子路径透传”
- 会应用 provider 级 service protocol 过滤
- 会参与请求级 failover
- 会记录请求日志、Prometheus 指标和 token usage

token usage 的特殊处理：

- 如果响应只报告 `prompt_tokens` + `total_tokens`
- 且 `total_tokens == prompt_tokens`
- 则会把 `completion_tokens` 规范化为 `0`

这样 embeddings 请求会被视为 token usage `exact`，而不是 `partial`

## 5. 不支持的内容

当前没有做这些事：

- Anthropic 专有 embeddings 报文
- embeddings 流式响应
- `anthropic_to_chat` 风格的 embeddings 协议转换
- 为 `anthropic` provider 伪造 embeddings 能力
