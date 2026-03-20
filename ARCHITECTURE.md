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

这里刻意不把启动期网络探测引入配置真相层。provider 协议如果省略，只允许按本地静态信号推断；probe 仍然只属于展示层。

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
- `provider.*.family` 必填；`provider.*.protocol` 仅作为兼容别名保留，不能与 `family` 冲突
- `provider.*.enabled_protocols` / `provider.*.disabled_protocols` 只负责在 family 候选协议面内做静态收缩，不改变 `route.protocol` 才是运行时真相
- `admin_password` / `api_keys` / `provider.*.api_key` 读取时兼容明文和 base64，写回配置时统一存为 base64；该兼容模式默认依赖当前支持的 secret 格式不会与规范化 base64 明文冲突
- `qwen` / `copilot` 若未显式设置 `api_key`，则从本地 `config_dir` 读取 OAuth 凭证
- `route.protocol` 是必填且唯一的 route 协议声明
- `route.exact_models.<name>` 直接声明 `upstreams`；`route.wildcard_models.<pattern>` 直接声明 `providers`
- `responses_stateful` exact model 只允许单 upstream；wildcard model 只允许单 provider
- 启用 `responses_to_chat` 的 provider 不能被 `responses_stateful` route 引用，因为它不支持 `previous_response_id`
- 不再接受 legacy `route.models` / `route.providers` / `route.system_prompts`
- `route` 的运行时派生字段只有在 `Validate()` 后可依赖

### 2. Gateway Layer

`internal/gateway` 是系统核心：

- 注册业务路由与管理端路由
- 在 `api_keys` 非空时校验客户端 API Key，并在转发前剥离客户端鉴权头
- 根据 route/model 选择 upstream provider
- 处理 OpenAI `chat/completions`、`responses` 与透明代理请求
- 在响应中提取工具调用并触发 route hook
- 记录日志、Prometheus 指标、按 API Key 的用量指标和 Dashboard 时序数据
- chat / responses 的请求日志拼装走共享 helper，保证流式响应在日志中尽量落为最终对象而不是原始 SSE 文本

`Gateway.Close()` 负责：

- 取消后台 context
- 停止 Dashboard 采样
- 关闭请求日志输出

### 3. Selector Layer

`internal/selector` 负责 provider 选择与状态维护：

- 按 route 模型编译结果选择候选 provider
- failover 的最小运行单元是单个 route model，不是整个 route
- 管理 provider 抑制窗口与失败计数
- 支持 failover
- 聚合 provider 状态供管理端展示
- 维护 provider 协议展示检测结果与 `provider + model + protocol` 精确探测结果
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
- 对流式推理请求先广播 `pending` 记录，再在完成时用同一 `request_id` 覆盖为最终记录，避免管理端日志页只能在长流结束后才看到请求
- 记录请求体，以及可解析时记录解压后的响应体（透明代理的 `gzip/br/zstd` 响应会先解压再落日志）
- 对同一客户端请求内发生的 failover 记录切换轨迹，保留失败 provider、下一跳 provider 与触发错误

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
- `provider.models` 在管理端中被定义为 provider 可用模型的静态基线与发现失败兜底，并复用运行时已发现模型作为录入建议来源；它不负责声明 route 对外公开模型面
- `Providers` 页面支持两类协议探测：
  - provider 级轻量检测，只更新展示协议，不进入运行时路由判断
  - `provider + model + protocol` 精确探测，用于确认某个模型是否真实可跑某个协议
- 管理端 provider 数据把 `configured_protocols` / `supported_protocols` 作为静态配置真相，把 `display_protocols` 作为轻量探测展示值；两者不能混用
- `Routes` 页面负责 route 配置编辑，包括 `exact_models` / `wildcard_models`
- `Routes` 页面必须先锁定 `route.protocol`，模型映射不再自带 `protocols` 分支
- route 编辑器里的 exact upstream model 建议会合并 `provider.models` 静态基线与当前运行时 `/models` 发现结果
- route 编辑器中的模型卡片采用左右分栏；当前 route 下所有模型都遵守同一个协议约束
- route 编辑器里的 exact upstream 列表默认按单行紧凑表格式排布，优先保证 provider / model 输入的横向连续性，窄屏再折行
- route 详情页采用高信息密度工作台布局：顶部概览展示聚合运行态，左侧主列先展示 exact model 摘要再进入编辑区，右侧摘要轨承接 provider 运行状态
- route 详情页上半区的 exact / wildcard 摘要表直接镜像当前可编辑配置；exact model 行提供“编辑 / 删除”入口，避免已配置模型只能回到卡片手工定位
- provider 运行数据不再单独占据底部大表，而是拆入顶部概览和右侧 provider 摘要卡片，减少监控视线往返
- 路由管理页里的自定义 combobox / tag suggestion 组件需要保留键盘导航和基础 ARIA 状态，不能只服务鼠标路径
- route model 的额外 system prompt 由 `prompt_enabled` 显式控制；UI 把开关放在模型主信息旁边，只有启用后才展开输入框；后端只在 `prompt_enabled=true` 且 `system_prompt` 非空时注入
- `Providers` 页面卡片可以直接发起“基于当前 provider 模型创建 route”；该入口会用 provider 的已配置/已发现模型预填 `exact_models`，并为新 route 预选单个协议
- route hooks 的主编辑入口是 `Tool Hooks` 页面
- `Config` 页面承载通用配置、客户端 API 密钥、webhook 与日志目标编辑
- `Config` 页面不承载 provider / route / hook 编辑
- `Chat` 页面按 `route.protocol` 选择请求协议：`chat -> /chat/completions`、`responses_* -> /responses`、`anthropic -> /messages`
- `Chat` 页面在 `responses_stateful` route 下会把返回的 `response.id` 保存在本地会话里，并在下一轮请求带上 `previous_response_id`；这只是管理端 UI 的本地状态，不是网关运行时状态
- `Logs` 页面在整合会话时，优先使用 Responses stateful 请求中的 `previous_response_id -> response.id` 显式关联；没有显式关联时再退回 fingerprint 前缀和旧的时间窗启发式
- `Logs` 页面按 `request_id` upsert SSE 事件，因此流式请求会先显示“进行中”，完成后在同一行补全 duration/response

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
3. 对已识别的推理端点做 route protocol 约束检查；未识别的非推理子路径保持透明透传
4. 复制请求头并清洗 hop-by-hop / 客户端认证头
5. 注入 provider 认证头和 `X-Forwarded-*`
6. 转发到 upstream
7. 记录日志和指标

## Key Design Decisions

### Route-Centric Model Surface

路由是外部 API 面；provider 只是内部能力来源。

因此：

- 外部暴露什么模型，由 `route.exact_models` 和 `route.wildcard_models` 决定
- 上游真实模型名，由 `upstreams[].model` 决定
- 通配符模型只决定 provider 候选集合，不改写请求模型名
- 一个配置中的单独 public model 可以通过多个 ordered upstream/provider 获得独立 failover，从而提供该模型自己的 HA，而不会把整个 route 绑成一个统一故障域

### Route Protocol Exposure

route 对外暴露哪些协议面，直接由 `route.protocol` 决定。

- `chat` 只暴露 `/chat/completions`
- `responses_stateless` 只承接无状态 `/responses`
- `responses_stateful` 承接原生 `/responses`，同时允许有状态和无状态请求
- `anthropic` 只暴露 `/messages`
- `responses_stateful` 在服务协议层同时覆盖 stateful/stateless `/responses`
- 运行时路由只依赖 `route.protocol + route model` 配置，不依赖 provider 卡片上的展示协议
- provider family 只承担“上游适配器”职责：`openai => chat + responses_*`，`anthropic => chat + anthropic`，`qwen/copilot/ollama => chat`
- `responses_to_chat` 只对无状态 Responses 请求生效；带 `previous_response_id` 的有状态请求只允许原生 `/responses` 透传，且禁用 failover

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

Admin SSE 接口统一返回 `X-Accel-Buffering: no` 与 `Cache-Control: no-cache, no-transform`，并在日志流建立后立即发送注释帧/空闲心跳，减少反向代理对小包 SSE 的缓冲延迟。

## Build Notes

- `Makefile` 是统一入口
- `make web` 构建前端静态资源
- `make build` 使用 `ldflags` 注入版本与构建日期
- `make test` 运行 `go vet` 与 `go test`
- 启动时配置加载仍走 `feconf`，但会覆盖默认 decode hook，避免上游默认值 hook 将显式 `false` 误判为零值并在 `prompt_enabled: false` 这类 `*bool` 字段上触发解码 panic
