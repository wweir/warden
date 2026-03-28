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

相关文档入口：

- 根使用说明：[README.md](/home/wweir/Mine/warden/README.md)
- 专题文档索引：[docs/README.md](/home/wweir/Mine/warden/docs/README.md)
- 配置说明：[config/README.md](/home/wweir/Mine/warden/config/README.md)

文档分工：

- `README.md` 负责项目入口、运行方式和最小使用示例
- `ARCHITECTURE.md` 负责系统边界、运行时分层和关键设计决策
- `docs/README.md` 负责仍需单独维护的专题文档索引
- `config/README.md` 负责配置模型和校验规则

## Directory Layout

```text
cmd/warden/          # 进程入口、信号处理、配置加载、版本注入
config/              # 配置模型、校验、路由运行时编译、示例配置
internal/
  gateway/           # HTTP 网关、协议桥接、指标聚合、管理面组装
    admin/           # 管理端 HTTP surface、嵌入式 SPA、配置/Provider/Route/Admin API
    httpmw/          # Recovery/CORS/API key 鉴权中间件
    logging/         # 请求日志后端构建与 attempt 日志辅助
    observe/         # 推理日志拼装、stream 组装、tool call 观测与 hook 分发
    proxy/           # 透明代理 surface、proxy 请求日志拼装、SSE 响应日志归一化
    requestctx/      # 请求级上下文元数据（client request/hooks/api key）
    snapshot/        # admin-facing metrics/api key 运行时快照组装
    telemetry/       # Prometheus collector、dashboard 时序存储、metrics helper
    upstream/        # 协议/传输适配、编码协商、转发头处理
  install/           # systemd 安装
  reqlog/            # 请求日志、指纹生成、JSON 清洗、SSE 广播
  selector/          # provider 选择、探活、抑制、failover 状态、模型发现
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
- `anthropic_to_chat` 只允许配置在 `openai` provider 上，并只影响 `route.protocol=anthropic` 的 `/messages` 入口
- 不再接受 legacy `route.models` / `route.providers` / `route.system_prompts`
- `route` 的运行时派生字段只有在 `Validate()` 后可依赖

### 2. Gateway Layer

`internal/gateway` 是系统核心：

- 注册业务路由与管理端路由
- 在 `api_keys` 非空时校验客户端 API Key，并在转发前剥离客户端鉴权头
- 根据 route/model 选择 upstream provider
- 处理 OpenAI `chat/completions`、`responses`、Anthropic `/messages` 与透明代理请求
- 在响应中提取工具调用并触发 route hook
- 记录日志、Prometheus 指标、按 API Key 的用量指标和 Dashboard 时序数据
- chat / responses / messages 的请求日志拼装走共享 helper，保证流式响应在日志中尽量落为最终对象而不是原始 SSE 文本
- `responses_to_chat` 与 `anthropic_to_chat` 的流式桥接采用逐帧 relay，不允许先完整缓冲上游 body 再回放
- 流式请求按三阶段记账：`pre_stream` 表示建流前失败，可 auth retry / failover；`in_stream` 表示开始向客户端 relay 后上游中断，只记 provider 失败；只有完整结束才记 success

当前内部进一步分成两个实现层：

- `gateway` 根包保留 HTTP surface、middleware 装配，以及少量必须从运行时状态读取的 admin callback 组装
- 根包启动期会先把 `route` 编译成按前缀长度降序的绑定表，HTTP 路由注册和透明代理 fallback 共用同一份有序视图，避免重叠前缀依赖 Go map 遍历顺序
- `internal/gateway/admin` 子包承载管理面路由、嵌入式前端分发，以及 provider 协议探测和 tool-hook suggestion 聚合等 admin-only 逻辑；只对 selector/broadcaster 和少量运行时快照回调做依赖注入，避免反向依赖根包
- `internal/gateway/admin` 包内再按 `router/auth`、`config`、`status+logs+metrics`、`providers`、`routes`、`apikeys` 分文件组织，避免管理面继续退化成单个超大 handler 文件
- `internal/gateway/httpmw` 子包承载 Recovery/CORS/API key 鉴权等通用 HTTP 中间件，避免根包继续混放基础 HTTP 基建
- `internal/gateway/logging` 子包承载请求日志后端构建与轻量 request attempt 日志，避免根包继续持有日志输出装配细节
- `internal/gateway/observe` 子包承载推理请求日志参数组装、stream log 归一化、tool call 解析与 hook 分发，避免根包继续混放响应观测逻辑
- `internal/gateway/proxy` 子包承载透明代理 handler、proxy provider 选择和 proxy 响应日志归一化，避免 fallback path 继续把 provider 选择、auth retry、SSE 日志拼装塞回根包
- `internal/gateway/requestctx` 子包承载请求级 context 元数据读写，避免 API key、原始请求句柄和 route hooks 的上下文键继续散落在根包
- `internal/gateway/snapshot` 子包承载 admin-facing metrics payload、dashboard counter sample 与 API key usage 汇总，避免根包继续混放只服务管理面的数据拼装
- `internal/gateway/bridge` 子包承载 SSE relay 和 Chat/Responses/Messages 之间的流式桥接，避免协议流转换细节继续堆在根包 handler 中
- `internal/gateway/inference` 子包承载 route-model 匹配、auth retry、failover trail 与当前 upstream target 状态，避免在 chat / responses / messages handler 内重复维护生命周期控制流
- 当某个 route model 只剩 1 个未被手动抑制的候选 provider 时，请求级重试会绕过自动抑制窗口，不再把该 provider 排除出本次请求，避免单 provider 路由被自动抑制直接打死
- `gateway` 根包内部再通过共享 inference session helper 统一 metric label 刷新、pending log 发布和 failover 后当前 target 切换，减少各协议入口重复脚手架代码
- `gateway` 根包内部对 `responses_to_chat` / `anthropic_to_chat` 再复用共享 chat-bridge helper，统一流式桥接重试、stream phase 记账和最终日志拼装
- `gateway` 根包内部对 `chat` / 原生 `responses` 再复用共享 buffered inference helper，统一一次性上游请求的准备、重试和日志写入
- 上述 buffered / relay helper 在 failover 后不会把请求锁死在旧执行分支；provider 切换到 bridge-capable 实现时，会重新走对应 bridge handler
- `internal/gateway/telemetry` 子包承载 Prometheus collector、label helper、dashboard rolling store 和 output rate tracker
- `internal/gateway/upstream` 子包承载协议 endpoint 映射、上游 HTTP 请求执行、请求/响应编解码、Accept-Encoding 协商和转发头清洗；根包不再直接持有 JSON/SSE upstream transport 细节
- admin metrics/API-key 回调直接指向 `internal/gateway/snapshot`，根包删除了只做一层转发的包装方法，减少无意义的运行时胶水代码

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
- 自动抑制只参与多 provider 竞争；若手动抑制导致某个 route model 已无可用 provider，selector 会先解除其它候选 provider 的自动抑制，再按原优先级继续选择
- 聚合 provider 状态供管理端展示
- 维护 provider 协议展示检测结果与 `provider + model + protocol` 精确探测结果
- provider 模型发现属于软失败路径；上游返回内部错误时日志做脱敏，不暴露原始 panic 细节
- 包内按职责拆成 `select`、`state`、`models`、`errors`、`types` 多文件，避免继续把选择策略、状态机和模型发现堆在单文件中

这里的设计原则是：路由决策与协议适配分离，避免在 handler 内部散落 provider 选择逻辑。

### 4. Protocol Layer

`pkg/protocol/openai` 与 `pkg/protocol/anthropic` 负责：

- 请求/响应结构定义
- 流式事件解析（含标准 SSE `event` / `data` / `id` / `retry` 与注释帧）
- OpenAI Chat / Responses 之间的协议转换
- Anthropic 请求与事件的兼容处理
- Chat ↔ Responses、Chat ↔ Messages 的流式桥接状态机，支持逐帧增量转换和流结束校验（如 `[DONE]`、`message_stop`）

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
- 包内按 `types`、`fingerprint`、`record sanitize/id`、`backend`、`broadcast` 分文件组织，避免日志模型、指纹算法和后端实现继续混写

Prometheus 指标与 Dashboard rolling store 由 `internal/gateway/telemetry` 维护，`internal/gateway` 根包负责把这些数据装配成 admin API 输出。Dashboard 读取两类数据：

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

其中 `Logs` 页面当前采用“左侧 session 树 + 右侧日志表 + 详情弹层”的排障结构：左侧按 route 分组展示 session chain，并支持整栏收起、route 分组折叠和视口内滚动，避免长会话把页面纵向撑长；右侧保留高密度日志列表，详情弹层拆成摘要、会话过程和响应结果三段，避免把 session 过滤、实时列表和请求排查混在同一平面里。

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
- `Logs` 页面在整合会话时，优先使用 Responses stateful 请求中的 `previous_response_id -> response.id` 显式关联；没有显式关联时只在同 route 下退回 fingerprint 前缀做保守归并，不再使用旧的 prompt 哈希 + 时间窗启发式，避免把独立请求误并成同一 session
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
- provider family 只承担“上游适配器”职责：`openai => chat + responses_*`，开启 `anthropic_to_chat` 时额外支持 `anthropic`；`anthropic => chat + anthropic`；`qwen/copilot/ollama => chat`
- `responses_to_chat` 只对无状态 Responses 请求生效；带 `previous_response_id` 的有状态请求只允许原生 `/responses` 透传，且禁用 failover
- `responses_to_chat` 只接受受控的 stateless Chat 兼容子集；不支持的 Responses 专有字段、非 `function` tools、未知 input item 在入口直接拒绝，不做 mock passthrough；兼容层会显式把 `max_output_tokens` 映射为 `max_completion_tokens`，校验/规范化 `tool_choice`，并允许 `function_call_output.output` 使用非字符串 JSON 值
- `responses_to_chat` 的 `instructions` 默认映射为首条 `developer` message；如果上游以 `400` 明确拒绝 `developer` role，bridge 会在同一 provider 上自动降级重试为 `system`
- `responses_to_chat` 的 Chat SSE -> Responses SSE 转换会补齐生命周期事件和关联元数据（如 `response.created` / `response.in_progress` / `response.output_item.done` / `output_index` / `item_id`），并在 done 事件中携带最终 item 快照，以提高对官方 SDK 状态机的兼容性
- `responses_to_chat` 在 Chat -> Responses 回写阶段会把 Chat `usage` 规范化为 Responses 风格的 `input_tokens` / `output_tokens`（并映射 `*_tokens_details`），同时把 Chat `finish_reason` 映射为 Responses `status` / `incomplete_details`
- `anthropic_to_chat` 只对 `route.protocol=anthropic` 的 `/messages` 请求生效；网关会把受控 Messages 子集转换为上游 Chat 请求，并把 Chat JSON / SSE 再转回 Anthropic Messages 形状
- `anthropic_to_chat` 明确不支持非文本 content block、`tool_result` 与普通 user text 混合块、以及未映射的 Anthropic 专有字段；这些请求在入口直接 `400`

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

## Document Map

当前专题文档：

- [docs/responses-stateful-stateless-support.md](/home/wweir/Mine/warden/docs/responses-stateful-stateless-support.md)：Responses stateless/stateful 与 `responses_to_chat` 的支持边界
- [docs/provider-dynamic-capability-discovery-plan.md](/home/wweir/Mine/warden/docs/provider-dynamic-capability-discovery-plan.md)：provider 能力展示与单协议 route 设计
- [docs/anthropic-messages-to-chat-plan.md](/home/wweir/Mine/warden/docs/anthropic-messages-to-chat-plan.md)：`anthropic_to_chat` 的桥接边界
- [docs/api-key-design.md](/home/wweir/Mine/warden/docs/api-key-design.md)：客户端 API Key 与敏感信息编码方案

## Build Notes

- `Makefile` 是统一入口
- `make web` 构建前端静态资源
- `make build` 使用 `ldflags` 注入版本与构建日期
- `make test` 运行 `go vet` 与 `go test`
- 启动时配置加载仍走 `feconf`，但会覆盖默认 decode hook，避免上游默认值 hook 将显式 `false` 误判为零值并在 `prompt_enabled: false` 这类 `*bool` 字段上触发解码 panic
