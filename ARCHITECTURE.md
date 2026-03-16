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

```text
cmd/warden/          # 进程入口、信号处理、配置加载、版本注入
config/              # 配置模型、校验、路由运行时编译、示例配置
internal/
  gateway/           # HTTP 网关、管理 API、指标、协议桥接、hook 集成
  install/           # systemd 安装
  reqlog/            # 请求日志、SSE 广播
  selector/          # provider 选择、探活、抑制、failover 状态
pkg/
  protocol/          # OpenAI / Anthropic 协议类型与转换
  provider/          # Qwen / Copilot OAuth token 管理
  toolhook/          # exec / ai / http hook 执行器
web/admin/           # Vue 3 管理端源码与构建产物
```

## Runtime Components

### 1. Config Layer

`config` 负责：

- 定义可序列化配置结构
- 在 `Validate()` 中做静态校验和规范化
- 将 route 配置编译为可直接匹配的运行时结构

主要配置块：

- `addr`
- `admin_password`
- `api_keys`
- `log`
- `webhook`
- `provider`
- `route`

约束：

- `provider.*.url` 与 `webhook.*.url` 必须为绝对 `http/https` URL
- `provider.*.proxy` 只允许 `http` / `https` / `socks5` / `socks5h`
- `qwen` / `copilot` 若未显式设置 `api_key`，则从本地 `config_dir` 读取 OAuth 凭证
- `route` 的运行时派生字段只有在 `Validate()` 后可依赖

### 2. Gateway Layer

`internal/gateway` 是系统核心：

- 注册业务路由与管理端路由
- 在 `api_keys` 非空时校验客户端 API Key，并在转发前剥离客户端鉴权头
- 根据 route/model 选择 upstream provider
- 处理 OpenAI `chat/completions`、`responses` 与透明代理请求
- 在响应中提取工具调用并触发 route hook
- 记录日志、Prometheus 指标、按 API Key 的用量指标和 Dashboard 时序数据

`Gateway.Close()` 负责：

- 取消后台 context
- 停止 Dashboard 采样
- 关闭请求日志输出

### 3. Selector Layer

`internal/selector` 负责 provider 选择与状态维护：

- 按 route 模型编译结果选择候选 provider
- 管理 provider 抑制窗口与失败计数
- 支持 failover
- 聚合 provider 状态供管理端展示
- provider 模型发现属于软失败路径；上游返回内部错误时日志做脱敏，不暴露原始 panic 细节

这里的设计原则是：路由决策与协议适配分离，避免在 handler 内部散落 provider 选择逻辑。

### 4. Protocol Layer

`pkg/protocol/openai` 与 `pkg/protocol/anthropic` 负责：

- 请求/响应结构定义
- 流式事件解析（含标准 SSE `event` / `data` / `id` / `retry` 与注释帧）
- OpenAI Chat / Responses 之间的协议转换
- Anthropic 请求与事件的兼容处理

协议层只处理协议，不处理路由、探活、日志或 hook 决策。

### 5. Provider Token Layer

`pkg/provider` 只负责 OAuth token 生命周期，不负责网络转发。

当前支持：

- `qwen`：从本地 `oauth_creds.json` 读取并自动 refresh
- `copilot`：从本地 `hosts.json` / `apps.json` 读取 GitHub OAuth token，再交换短期 Copilot token

设计决策：

- token 管理与 HTTP 转发解耦
- 不再支持远程凭证读取
- 刷新后的凭证只回写本地文件

### 6. Tool Hook Layer

`pkg/toolhook` 执行三类 hook：

- `exec`：把调用上下文 JSON 写入子进程 stdin
- `ai`：回调网关自身 route/model 做策略判断
- `http`：调用 `config.webhook` 中定义的 webhook

Hook 是 route-scoped 的，读取来源只有 `route.<prefix>.hooks`。

注意：

- Warden 只观察模型返回的工具调用
- Warden 不负责外部工具进程注册、发现或调用
- 工具名仍可能带 `<prefix>__<name>` 命名空间，`MCPName` 只是名字拆分结果，不代表系统内仍有 MCP runtime

### 7. Logging and Metrics

`internal/reqlog` 负责：

- 文件 / HTTP 双后端日志输出
- SSE 广播
- 管理端日志流消费

Prometheus 指标由 `internal/gateway` 维护，Dashboard 读取两类数据：

- 即时聚合指标
- 内存中的滚动时间序列

这样做避免前端直接处理累积 counter 差分。

### 8. Admin UI

`web/admin` 是嵌入式 Vue 3 控制台，提供：

- Dashboard
- Providers
- Routes
- Tool Hooks
- Logs
- Config

管理端只消费当前后端暴露的能力；不再展示 MCP / SSH 页面与配置区块。

配置编辑的当前边界：

- `Providers` 页面负责单个 provider 配置编辑
- `Routes` 页面负责 route 配置编辑，包括 `exact_models` / `wildcard_models`
- route hooks 的主编辑入口是 `Tool Hooks` 页面
- `Config` 页面承载通用配置、客户端 API 密钥、webhook 与日志目标编辑
- `Config` 页面不承载 provider / route / hook 编辑

## Request Flow

### Chat / Responses Request

1. 命中 route
2. 若配置了 `api_keys`，先校验客户端 API Key
3. 解析请求中的公开模型名
4. 根据 route 编译结果选择 upstream provider/model
5. 构造上游请求，并按需做协议桥接或 system prompt 注入
6. 转发到 upstream；客户端 API Key 头不会透传给 provider
7. 解析响应中的工具调用
8. 对命中的 tool call 执行 route hook
9. 记录日志、更新指标、返回客户端

### Transparent Proxy Request

1. 按 route 前缀匹配
2. 若配置了 `api_keys`，先校验客户端 API Key
3. 复制请求头并清洗 hop-by-hop / 客户端认证头
4. 注入 provider 认证头和 `X-Forwarded-*`
5. 转发到 upstream
6. 记录日志和指标

## Key Design Decisions

### Route-Centric Model Surface

路由是外部 API 面；provider 只是内部能力来源。

因此：

- 外部暴露什么模型，由 `route.exact_models` 和 `route.wildcard_models` 决定
- 上游真实模型名，由 `upstreams[].model` 决定
- 通配符模型只决定 provider 候选集合，不改写请求模型名

### Hook Boundary Instead of Tool Runtime

旧设计把网关推进到了工具运行时管理层，这导致：

- 责任边界模糊
- 管理面板与配置复杂度膨胀
- 对非工具路径没有收益，却引入大量额外故障面

当前设计收缩为“只观察和审计工具调用”。这是更稳的边界。

### Local-Only Credential Access

OAuth 凭证读取现在只走本地文件：

- 配置更简单
- 校验路径更直
- 失败模式更可预测
- 避免 SSH 带来的外部依赖和隐式环境耦合

## Admin API Surface

当前管理端关键接口：

- `GET /_admin/api/status`
- `GET /_admin/api/config/source`
- `GET /_admin/api/config`
- `PUT /_admin/api/config`
- `POST /_admin/api/config/validate`
- `POST /_admin/api/restart`
- `GET /_admin/api/providers/detail`
- `POST /_admin/api/providers/health`
- `POST /_admin/api/providers/suppress`
- `GET /_admin/api/routes/detail`
- `GET /_admin/api/tool-hooks/suggestions`
- `GET /_admin/api/logs/stream`
- `GET /_admin/api/metrics/stream`
- `GET|POST|DELETE /_admin/api/apikeys`

`/_admin/api/mcp/*` 已移除。

## Build Notes

- `Makefile` 是统一入口
- `make web` 构建前端静态资源
- `make build` 使用 `ldflags` 注入版本与构建日期
- `make test` 运行 `go vet` 与 `go test`
