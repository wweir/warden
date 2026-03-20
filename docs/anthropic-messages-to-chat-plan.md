# Anthropic Messages To Chat 桥接

> 更新日期：2026-03-20

本文记录 `anthropic_to_chat` 的已落地设计与实现边界。

## 1. 目标

在不改变客户端调用方式的前提下，让外部 `POST /messages` 请求可以按 Anthropic Messages 语义进入网关，但实际转发到上游 OpenAI `POST /chat/completions`。

要求：

- 保持 route/model 选择、failover、鉴权重试、指标、请求日志、tool hook 继续工作
- 非流式返回 Anthropic Messages JSON
- 流式返回 Anthropic Messages SSE
- 不伪装成“完整 Anthropic 协议兼容”，而是明确受控子集

## 2. 配置开关

新增 provider 级布尔开关：

```yaml
provider:
  openai:
    family: openai
    url: "https://api.openai.com/v1"
    anthropic_to_chat: true
```

约束：

- 只允许配置在 `openai` provider 上
- 仅影响 `route.protocol=anthropic` 的 `/messages` 入口
- 不改变 `chat` / `responses*` 路径行为

## 3. 受支持子集

### 3.1 请求

支持：

- 顶层 `model`
- 顶层 `system`：字符串或纯文本 blocks
- `messages`
  - `user`：字符串或纯文本 blocks
  - `assistant`：文本 blocks + `tool_use`
  - `user` 的 `tool_result`
- `tools`
- `tool_choice`
- `max_tokens`
- `temperature`
- `top_p`
- `metadata`
- `stop_sequences`
- `stream`

不支持：

- 非文本 content block
- `tool_result` 与普通 user text 混合在同一条消息里
- `is_error=true` 的 `tool_result`
- 未映射的 Anthropic 专有字段

不支持的输入在入口直接返回 `400`。

### 3.2 响应

- OpenAI Chat `message.content` -> Anthropic `content[type=text]`
- OpenAI Chat `message.tool_calls` -> Anthropic `content[type=tool_use]`
- `finish_reason`
  - `stop` -> `end_turn`
  - `tool_calls` -> `tool_use`
  - `length` -> `max_tokens`

### 3.3 流式

网关把 OpenAI Chat chunk 流转换为 Anthropic SSE：

- 首块 -> `message_start`
- 文本 delta -> `content_block_start/delta/stop`
- `tool_calls` delta -> `content_block_start(type=tool_use)` + `input_json_delta`
- 结束块 -> `message_delta`
- 尾块 -> `message_stop`

## 4. 网关实现

- `route.protocol=anthropic` 现在注册专用 `/messages` handler，而不是完全依赖 transparent proxy
- 原生 `anthropic` provider 继续直连 `/messages`
- 启用 `anthropic_to_chat` 的 `openai` provider 在 handler 内做：
  - `Messages -> Chat` 请求转换
  - `Chat -> Messages` 响应转换
  - `Chat SSE -> Messages SSE` 流转换
- 两条路径共用 selector、failover、日志、指标、tool hook

## 5. 风险与边界

- 该桥接不保证完整 Anthropic 兼容，只保证受控子集可预测
- 如果同一 anthropic route model 混用原生 `anthropic` provider 与 `anthropic_to_chat` provider，语义交集仍以“桥接子集”为准
- 当前实现优先保证请求/响应语义闭合，不尝试桥接 Anthropic 其他专有子接口
