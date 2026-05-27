# Warden Architecture

## Overview

Warden 是一个 route-centric 的 AI Gateway。

核心目标：

- 对外暴露统一的 OpenAI 兼容入口
- 按 `route` 和 `model` 把请求映射到不同 upstream provider
- 在不污染协议适配层的前提下，对模型返回的工具调用执行 route-scoped hook
- 提供管理面板、请求日志、Prometheus 指标与运行时热重载

当前版本已经移除两类能力：

- 内置 MCP client 运行时
- SSH 远程执行与 `ssh` 配置块

这不是表面删字段，而是收缩边界：Warden 现在只负责网关、协议、路由、hook、日志和观测，不再负责外部工具进程生命周期管理。

## Directory Layout

```
cmd/warden/          # 进程入口、信号处理、配置加载、版本注入
config/              # 配置模型、校验、路由运行时编译、示例配置
internal/
  providerauth/      # provider 出站鉴权头和静态 header 注入
  gateway/           # HTTP 网关、协议桥接、指标聚合、管理面组装
    admin/           # 管理端 HTTP surface、嵌入式 SPA、配置/Provider/Route/Admin API
    bridge/          # SSE relay 与 Chat/Responses/Messages 流式桥接
    httpmw/          # Recovery/CORS/API key 鉴权中间件
    inference/       # route-model 匹配、service protocol 分类、auth retry、failover trail
    observe/         # 推理日志拼装、SSE 日志归一化、tool call 观测与 hook 分发
    proxy/           # 透明代理 surface、proxy provider 选择和 proxy 请求日志拼装
    requestctx/      # 请求级上下文元数据（client request/hooks/api key）
    telemetry/       # Prometheus collector、dashboard 时序存储、metrics helper
    tokenusage/      # JSON/SSE token usage 解析与完整性归一化
    upstream/        # 协议/传输适配、编码协商、转发头处理
  install/           # 托管安装辅助（systemd / launchd / Task Scheduler）
  reqlog/            # 请求日志、指纹生成、JSON 清洗、SSE 广播
    fingerprint/     # 请求体 fingerprint 提取与紧凑哈希构建
  selector/          # provider 选择、探活、抑制、failover 状态、模型发现
pkg/
  protocol/          # OpenAI / Anthropic 协议类型与转换
  provider/          # Copilot OAuth token 管理
  toolhook/          # exec / ai / http hook 执行器
web/admin/           # Vue 3 管理端源码与构建产物
```

## Runtime Components

### 1. Config Layer

`config` 负责：

- 定义可序列化配置结构
- 在 `Validate()` 中做静态校验和规范化
- 将 route 配置编译为可直接匹配的运行时结构

主要配置块：`addr`, `admin_password`, `log`, `webhook`, `cliproxy`, `provider`, `route`

### 2. Gateway Layer

`internal/gateway` 是系统核心：

- 注册业务路由与管理端路由
- 在命中 route 且 `route.api_keys` 非空时校验客户端 API Key
- 根据 route/model 选择 upstream provider
- 处理 OpenAI `chat/completions`、`responses`、Anthropic `/messages` 与透明代理请求
- 在响应中提取工具调用并触发 route hook
- 记录日志、Prometheus 指标、按 API Key 的用量指标和 Dashboard 时序数据

内部进一步分成多个子包：`admin`, `bridge`, `httpmw`, `inference`, `observe`, `proxy`, `requestctx`, `telemetry`, `tokenusage`, `upstream`

### 3. Selector Layer

`internal/selector` 负责 provider 选择与状态维护：

- 按 route 模型编译结果选择候选 provider
- 管理 provider 抑制窗口与失败计数
- 支持 failover
- 聚合 provider 状态供管理端展示

### 4. Protocol Layer

`pkg/protocol/openai` 与 `pkg/protocol/anthropic` 负责：

- 请求/响应结构定义
- 流式事件解析
- OpenAI Chat / Responses 之间的协议转换
- Anthropic 请求与事件的兼容处理

### 5. Provider Token Layer

`pkg/provider` 只负责 OAuth token 生命周期，不负责网络转发。当前支持 `copilot`：

- 从本地 `hosts.json` / `apps.json` 读取 GitHub OAuth token
- 交换短期 Copilot token

### 6. Tool Hook Layer

`pkg/toolhook` 执行三类 hook：

- `exec`：把调用上下文 JSON 写入子进程 stdin
- `ai`：回调网关自身 route/model 做策略判断
- `http`：调用 `config.webhook` 中定义的 webhook

### 7. Logging and Metrics

`internal/reqlog` 负责：

- 文件 / HTTP 双后端日志输出
- SSE 广播
- 管理端日志流消费

`internal/gateway/telemetry` 负责：

- Prometheus collector
- Dashboard rolling store

### 8. Admin UI

`web/admin` 是嵌入式 Vue 3 控制台，提供 Dashboard, Providers, Routes, Tool Hooks, Logs, Chat, Config 等页面。

## Request Flow

### Chat / Responses Request

1. 命中 route
2. 若配置了 `route.api_keys`，先校验客户端 API Key
3. 解析请求中的公开模型名
4. 根据 route 编译结果选择 upstream provider/model
5. 构造上游请求，并按需做协议桥接或 system prompt 注入
6. 转发到 upstream
7. 解析响应中的工具调用，执行 route hook
8. 记录日志、更新指标、返回客户端

### Transparent Proxy Request

1. 按 route 前缀匹配
2. 若配置了 `route.api_keys`，校验客户端 API Key
3. 对已识别的推理端点做 route protocol 约束检查
4. 复制请求头并清洗 hop-by-hop / 客户端认证头
5. 注入 provider 认证头和 `X-Forwarded-*`
6. 转发到 upstream
7. 记录日志和指标

## Key Design Decisions

### Route-Centric Model Surface

路由是外部 API 面；provider 只是内部能力来源。

- 外部暴露什么模型，由 `route.exact_models` 和 `route.wildcard_models` 决定
- 上游真实模型名，由 `upstreams[].model` 决定
- 通配符模型只决定 provider 候选集合，不改写请求模型名

### Route Protocol Exposure

- `chat` 暴露 `/chat/completions`；当 route 至少包含一个支持 embeddings 的 upstream/provider 时，额外暴露 `/embeddings`
- `responses` 暴露 `/responses`
- `anthropic` 暴露 `/messages`

### Hook Boundary Instead of Tool Runtime

旧设计把网关推进到了工具运行时管理层。当前设计收缩为"只观察和审计工具调用"。

### Local-Only Credential Access

OAuth 凭证读取现在只走本地文件，简化配置和失败模式。

## Admin API Surface

关键接口：

- `GET /_admin/api/status` - 系统状态
- `GET /_admin/api/config/source` - 配置文件路径
- `GET /_admin/api/config` - 配置内容
- `PUT /_admin/api/config` - 更新配置
- `POST /_admin/api/config/validate` - 校验配置
- `POST /_admin/api/restart` - 重启服务
- `GET /_admin/api/providers/detail` - Provider 详情
- `POST /_admin/api/providers/health` - 健康检查
- `POST /_admin/api/providers/suppress` - 手动抑制
- `POST /_admin/api/providers/probe-access` - 协议探测
- `GET /_admin/api/routes/detail` - Route 详情
- `GET /_admin/api/tool-hooks/suggestions` - Hook 建议
- `GET /_admin/api/logs/stream` - 日志流
- `GET /_admin/api/metrics/stream` - 指标流

## Related Documentation

- [README.md](./README.md) - 项目入口、运行方式和最小使用示例
- [OPERATIONS.md](./OPERATIONS.md) - 部署配置、运维规范、平台差异
- [docs/README.md](./docs/README.md) - 专题文档索引
- [config/README.md](./config/README.md) - 配置模型和校验规则
- [docs/provider.md](./docs/provider.md) - Provider 子系统配置模型
- [docs/responses-protocol-support.md](./docs/responses-protocol-support.md) - `responses` 协议实现边界
