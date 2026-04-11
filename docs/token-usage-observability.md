# Token 用量观测实现

> 更新日期：2026-04-08
>
> 状态：current

本文只描述运行观测层的 token 用量实现，不包含配额控制、账单对账或持久化 ledger。

## 1. 目标

当前实现只解决一件事：

1. 让 route / provider / API key 维度的 token 用量与吞吐基于协议级 usage 信号记账，而不是继续依赖最终日志响应体的猜测解析

## 2. 当前实现

### 2.1 tokenusage 子包

新增 `internal/gateway/tokenusage`：

- 解析非流式 JSON 响应里的 `usage`
- 解析 OpenAI Chat SSE chunk usage
- 解析 Responses SSE 中的 `response.usage`
- 解析 Anthropic `message_start` / `message_delta` usage
- 统一输出 `exact` / `partial` / `missing` 三种完整性

该包只负责协议级 usage 观察，不负责 Prometheus 或日志写入。

### 2.2 计量策略

Prometheus token counter 现在只累计 `exact` 观测：

- `warden_route_tokens_total`
- `warden_provider_tokens_total`
- `warden_apikey_tokens_total`

这样做的原因很直接：半截流、缺失 usage、或只拿到一半 usage 的请求，不应该被伪装成完整 token 统计。

### 2.3 coverage 指标

新增 token observation coverage counter：

- `warden_route_token_observations_total`
- `warden_provider_token_observations_total`
- `warden_apikey_token_observations_total`

这些指标按 `completeness + source` 聚合，显式暴露：

- 哪些请求拿到了精确 usage
- 哪些请求只有部分 usage
- 哪些请求完全缺失 usage

### 2.4 请求日志

`reqlog.Record` 新增 `token_usage`：

- `prompt_tokens`
- `completion_tokens`
- `total_tokens`
- `source`
- `completeness`

日志里现在可以直接判断某条请求为什么没有进入精确 token counter，而不用再反查原始流内容。

### 2.5 Admin 快照

Admin metrics snapshot 额外暴露：

- `token_observations_total`
- `route_token_observations_total`
- `provider_token_observations_total`

API key payload 额外暴露：

- `exact_usage_requests`
- `partial_usage_requests`
- `missing_usage_requests`

## 3. 明确不包含的能力

以下能力仍然不在当前实现范围内：

- tokenizer 估算
- quota / rate limit / budget enforcement
- 持久化 token ledger
- 账单对账

这些需求需要另一套设计，不能继续复用当前进程内 Prometheus counter 近似冒充。

## 4. 相关代码

- [internal/gateway/tokenusage/tokenusage.go](/home/wweir/Mine/warden/internal/gateway/tokenusage/tokenusage.go)
- [internal/gateway/telemetry/metrics.go](/home/wweir/Mine/warden/internal/gateway/telemetry/metrics.go)
- [internal/gateway/observe/observe.go](/home/wweir/Mine/warden/internal/gateway/observe/observe.go)
- [internal/gateway/snapshot/snapshot.go](/home/wweir/Mine/warden/internal/gateway/snapshot/snapshot.go)
- [internal/reqlog/types.go](/home/wweir/Mine/warden/internal/reqlog/types.go)
