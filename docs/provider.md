# Provider

> 状态：draft（2026-05-18）
>
> 本文档是 Warden provider 子系统的一站式技术文档，涵盖配置模型、协议格式、路由设计、能力探测、创建体验与设计决策。

## 目录

1. [概述](#1-概述)
2. [配置模型](#2-配置模型)
3. [协议与格式](#3-协议与格式)
4. [Route 单协议设计](#4-route-单协议设计)
5. [运行时真相分层](#5-运行时真相分层)
6. [探测策略](#6-探测策略)
7. [创建体验](#7-创建体验)
8. [设计决策](#8-设计决策)

---

## 1. 概述

`provider.*` 是 Warden 的配置真相源。一个 provider 代表一个上游供应商账户。

核心设计原则：

- **配置真相唯一**：`provider.*` TOML 是唯一真相，admin UI 的创建向导和探测结果只是辅助输入层
- **账户即 provider**：一个 provider 对应一个认证账户（一个 API Key），一个账户下可以有多个协议入口
- **endpoint 是协议入口**：每个 endpoint 是一个完整的协议接入定义（URL + format + protocols），不再是"覆盖层"
- **单协议 route**：每个 route 锁定一个协议，不再嵌套多协议模型映射
- **探测不入配置**：强探测必须由用户显式触发，探测结果只进入 UI 建议，不自动写入配置

---

## 2. 配置模型

### 2.1 推荐 TOML 形态

**国内主流 provider（最简配置）：**

```toml
[provider.kimi]
url = "https://api.moonshot.cn/v1"
api_key = "sk-provider-token"
models = ["kimi-k2", "kimi-k2.5"]
```

默认推导：`format = "openai"`，`protocols = ["chat", "responses", "embeddings"]`。单 endpoint 场景无需显式声明 endpoint。

**收窄能力（如 Ollama 只支持 chat）：**

```toml
[provider.ollama]
url = "http://127.0.0.1:11434/v1"
protocols = ["chat"]
```

**Anthropic-native provider：**

```toml
[provider.anthropic]
url = "https://api.anthropic.com/v1"
format = "anthropic"
api_key = "sk-ant-token"
```

默认推导：`protocols = ["chat", "anthropic"]`。

**桥接：让 anthropic route 也能走 OpenAI-format provider：**

```toml
[provider.deepseek]
url = "https://api.deepseek.com/v1"
api_key = "sk-provider-token"
anthropic_to_chat = true
```

**Coding Plan 多 endpoint（一个账户，两个协议入口）：**

```toml
[provider.bailian]
api_key = "sk-sp-xxxxx"
models = ["kimi-k2.5", "glm-5", "deepseek-chat"]

[provider.bailian.endpoint.openai]
url = "https://coding.dashscope.aliyuncs.com/v1"
format = "openai"
protocols = ["chat", "responses", "embeddings"]

[provider.bailian.endpoint.anthropic]
url = "https://coding.dashscope.aliyuncs.com/apps/anthropic"
format = "anthropic"
protocols = ["chat", "anthropic"]
```

**企业统一网关（多个相同 format 的不同入口）：**

```toml
[provider.corp]
api_key = "sk-corp-token"
models = ["gpt-4o", "claude-sonnet"]

[provider.corp.endpoint.domestic]
url = "https://gateway.corp.cn/openai/v1"
format = "openai"
protocols = ["chat"]

[provider.corp.endpoint.overseas]
url = "https://gateway.corp.com/openai/v1"
format = "openai"
protocols = ["chat", "embeddings"]
anthropic_to_chat = true
```

### 2.2 字段层级

Provider 级字段（账户级，所有 endpoint 共享）：

- `api_key` / `api_key_command` — 鉴权凭证
- `proxy` — 网络代理
- `headers` — 公共请求头（所有 endpoint 都会携带）
- `models` — 静态模型基线（所有 endpoint 共享，可被 endpoint 级覆盖）
- `disabled` — 手动禁用整个 provider
- `timeout` — 首 token 超时
- `backend` / `backend_provider` — cliproxy 后端标记
- `config_dir` — Copilot 等场景的本地凭证目录

> 当 provider 显式声明了 `endpoint.*` 时，`provider.url`、`provider.format`、`provider.protocols` 必须为空。Validate 会报错阻止混合写法。

Endpoint 级字段（每个 endpoint 独立的协议定义）：

- `url` — 该 endpoint 的 base URL（必填）
- `format` — 上游原生协议格式：`openai`（默认）、`anthropic`、`copilot`
- `protocols` — 该 endpoint 支持的服务协议列表；省略时按 `format` 推导默认值
- `headers` — 在 provider 级 `headers` 之后合并，允许覆盖同名 header
- `models` — 覆盖 provider 级 `models`
- `anthropic_to_chat` — 桥接开关：允许 `anthropic` route 走此 OpenAI-format endpoint
- `responses_to_chat` — 桥接开关：允许 `responses` route 走此 OpenAI-format endpoint（仅无状态请求）
- `anthropic_to_responses` — 桥接开关：允许 `responses` route 走此 Anthropic-format endpoint（仅无状态请求）

简写规则（单 endpoint 场景）：

- `endpoint` 为空时，`provider.url` + `provider.format`（默认 openai）+ `provider.protocols` 自动构成一个隐式的 `default` endpoint
- `endpoint` 非空时，provider 级不能再写 `url`、`format`、`protocols`

### 2.3 能力推导规则

`format` 省略时默认 `openai`。

`protocols` 省略时按 `format` 推导：

| `format` | 默认 `protocols` |
|---|---|
| `openai` | `chat`, `responses`, `embeddings` |
| `anthropic` | `chat`, `anthropic` |
| `copilot` | `chat` |

桥接开关生效后自动追加对应协议：

| 桥接开关 | 生效条件 | 追加协议 |
|---|---|---|
| `anthropic_to_chat` | `format = "openai"` | `anthropic` |
| `responses_to_chat` | `format = "openai"` | 已包含在默认值中，无额外效果 |
| `anthropic_to_responses` | `format = "anthropic"` | `responses` |

显式 `protocols` 非空时以显式值为准，不再追加默认值；桥接开关只让对应协议成为合法候选，如果显式 `protocols` 没有包含它，该 endpoint 仍不参与对应 route。

### 2.4 OpenAI URL 兼容规则

OpenAI-compatible 生态里用户可能输入 `https://provider.example.com` 或 `https://provider.example.com/v1`，探测和保存流程需兼容：

1. 先用用户输入的 URL 原样生成探测候选
2. 如果 URL path 不以 `/v1` 结尾，追加 `path + /v1` 候选
3. 如果 URL path 以 `/v1` 结尾，追加去掉尾部 `/v1` 候选
4. 按候选顺序探测，成功则 UI 显示为 `resolved_openai_url`
5. 保存时写入用户确认后的有效 URL
6. 运行时只使用保存后的 URL，不在 as-is 与 `/v1` 之间 failover
7. 如果两者都可用，UI 提示用户显式确认最终保存值

此兼容逻辑只属于 `format = "openai"` endpoint 的探测与保存流程；`format = "anthropic"` 不自动套用。

### 2.5 Backend / cliproxy 边界

`backend = "cliproxy"` 表示 Warden 访问本地或嵌入式 CLIProxyAPI 的 OpenAI-compatible sidecar：

- `format` 必须为 `openai`（或省略，默认就是 `openai`）
- `backend_provider` 表示 CLIProxyAPI 内部 provider 名称
- cliproxy 认证由 `cliproxy.auth_dir` 管理，不允许配置 provider 级或 endpoint 级 API key 命令
- cliproxy 的 URL 是 Warden 到 CLIProxyAPI 服务的内部 HTTP 边界，普通预设路径隐藏该字段
- cliproxy 认证导入只做离线结构校验，不在导入路径中刷新 token 或访问上游；在线验证必须由用户手动触发

---

## 3. 协议与格式

### 3.1 OpenAI Format

`format = "openai"`（默认）表示上游原生协议是 OpenAI-compatible。

默认支持协议：`chat`、`responses`、`embeddings`。

Endpoint 映射：

| Protocol | 上游 Endpoint |
|---|---|
| `chat` | `/chat/completions` |
| `responses` | `/responses` |
| `embeddings` | `/embeddings` |
| `anthropic`（桥接） | `/chat/completions` |

鉴权：默认 `Authorization: Bearer <api_key>`，provider 级 + endpoint 级 `headers` 可覆盖或补充。

### 3.2 Anthropic Format

`format = "anthropic"` 表示上游原生协议是 Anthropic-compatible。

默认支持协议：`chat`、`anthropic`。

Endpoint 映射：

| Protocol | 上游 Endpoint | 备注 |
|---|---|---|
| `chat` | `/messages` | Warden 使用 chat IR 转 Anthropic Messages |
| `anthropic` | `/messages` | |
| `responses`（桥接） | `/messages` | Warden 做 Responses ↔ Chat IR ↔ Messages 桥接 |

鉴权：

- `x-api-key: <api_key>`
- `anthropic-version: 2023-06-01`
- `Authorization: Bearer <api_key>`（保留给 Anthropic-compatible 代理使用）
- Provider 级 + endpoint 级 `headers` 可覆盖或补充

### 3.3 Copilot

Copilot 不使用 `endpoint` 模型，能力固定为 `chat`。不默认支持 `responses`。

### 3.4 桥接

桥接是 Warden 网关的能力，不是 provider 的能力。桥接开关放在 endpoint 级后，语义变为："这个 endpoint 本身支持某些协议，Warden additionally 可以通过桥接让它也支持额外的 route 协议"。

| 桥接开关 | 适用 `format` | 效果 |
|---|---|---|
| `anthropic_to_chat` | `openai` | `anthropic` route → 上游 `/chat/completions`，Warden 做 Messages ↔ Chat 桥接 |
| `responses_to_chat` | `openai` | 无状态 `responses` route → 上游 `/chat/completions`，Warden 做 Responses ↔ Chat 桥接 |
| `anthropic_to_responses` | `anthropic` | 无状态 `responses` route → 上游 `/messages`，Warden 做 Responses ↔ Chat IR ↔ Messages 桥接 |

`responses_to_chat` 和 `anthropic_to_responses` 只对无状态 Responses 请求生效；带 `previous_response_id` 的有状态请求会绕过桥接，直接进入透明转发链路，失败时不做 failover。

### 3.5 协议能力总览

| Format | chat | responses | anthropic | embeddings |
|---|---|---|---|---|
| `openai` | 原生 | 原生 | 桥接 (`anthropic_to_chat`) | 原生 |
| `anthropic` | 原生 | 桥接 (`anthropic_to_responses`) | 原生 | — |
| Copilot | 原生 | — | — | — |

---

## 4. Route 单协议设计

### 4.1 结构

每个 route 锁定唯一协议：

```toml
[route."/openai"]
protocol = "chat"

[route."/openai".exact_models.gpt-4o]
[[route."/openai".exact_models.gpt-4o.upstreams]]
provider = "openai"
model = "gpt-4o"

[route."/openai".wildcard_models."gpt-*"]
providers = ["openai"]
```

约束：

- `route.protocol` 必填，且只允许一个值：`chat`、`responses`、`anthropic`
- `exact_models` 不再嵌套 `protocols`
- `wildcard_models` 不再嵌套 `protocols`
- Route 内所有模型映射受 `route.protocol` 统一约束

### 4.2 Route 协议匹配

当某个 provider 支持的路由协议需要桥接时，selector 按以下规则生成候选：

1. 配置中显式排序的 provider 顺序优先
2. 同 provider 内原生协议优先于桥接协议
3. 同 provider 内 endpoint 按 format 默认排序：
   - `chat` route：`openai` format 优先
   - `responses` route：原生 `openai responses` 优先，其次 `anthropic_to_responses`，最后 `responses_to_chat`
   - `anthropic` route：原生 `anthropic` format 优先，其次 `openai` + `anthropic_to_chat`
4. 被禁用或被抑制窗口排除的 provider/endpoint 不生成候选
5. 不引入 per-route format override（若需要强制走某个 endpoint，可用两个 provider 配置作为过渡）

### 4.3 Responses 约束

- `responses` 协议同时承接无状态请求（无 `previous_response_id`）和有状态请求（带 `previous_response_id`）
- 无状态请求走 inference handler，支持 failover、tool hooks、`responses_to_chat` 桥接
- 有状态请求走透明转发链路，禁用 failover，不解析 tool calls
- 网关本身不维护 Responses 会话状态；`previous_response_id` 的语义完全依赖上游 provider

---

## 5. 运行时真相分层

真相分两层，不允许混淆：

### 5.1 展示层

Provider 详情页展示四类信息：

- `candidate_protocols`：按所有 endpoint 的 `format` + 桥接开关静态推导的候选协议面
- `configured_protocols`：当前配置下真正允许声明 route 的协议面
- `display_protocols`：从 `configured_protocols` 静态推导的 UI 展示列表（含 embeddings 独立展示）
- `provider + endpoint + model + protocol` 精确 probe 结果（需用户手动触发网络探测）

这些结果：

- `candidate_protocols` / `configured_protocols` / `display_protocols` 来自本地静态规则，不依赖网络
- 精确 probe 结果依赖网络探测，由用户手动触发

### 5.2 路由层

真正决定运行时的是：

1. `route.protocol`
2. `route.exact_models` / `route.wildcard_models`
3. Provider endpoint 的 `format` 与 `protocols` 配置

- Route 能暴露什么入口，只看 `route.protocol`
- 某个 public model 指向哪些上游，只看该 route model 配置
- Provider 卡片上的展示协议不会改变 selector / gateway 的选择结果
- 配置校验不依赖启动期网络

### 5.3 运行时选择结果

选择 provider 时的五元组：

```text
provider + endpoint_name + format + model + service_protocol
```

请求执行路径从 target 中读取 endpoint 和 format：

- endpoint 映射使用 `ProtocolEndpoint(format, serviceProtocol, bridge)`
- 鉴权头注入使用 `providerauth.SetHeaders` + endpoint 参数
- 请求编组使用 `upstream.MarshalProtocolRequest` + endpoint 参数
- 桥接开关在 endpoint 级
- 日志与指标增加 `provider_format` 和 `provider_endpoint` label

### 5.4 Selector 状态

Selector 状态以 `provider + endpoint_name` 为 key：

- 每个 endpoint 独立维护健康状态、模型发现结果、协议探测状态
- 自动抑制按 endpoint 记录（Anthropic endpoint 失败不会抑制同一 provider 的 OpenAI endpoint）
- 手动抑制保留 provider 级总开关
- 模型发现对每个 enabled endpoint 分别请求 `<url>/models`
- Route `/models` 合并时按 route service protocol 选择可用 endpoint

---

## 6. 探测策略

探测分为三层，不得混成一个"自动启用"动作。

### 6.1 连接探测

输入：`url`、`api_key`（或 `api_key_command`）、`proxy`、`headers`

输出：网络可达性、鉴权是否明显失败、TLS / proxy / DNS 错误

连接探测只证明 base URL 与凭证可用，不证明具体协议可用。

### 6.2 格式探测

按 endpoint `format` 使用有效 base URL。`format = "openai"` 探测必须按 [OpenAI URL 兼容规则](#24-openai-url-兼容规则) 生成 as-is 与 `/v1` 候选；`format = "anthropic"` 只使用显式配置的 URL。

**OpenAI-compatible 候选：**

| 探测 | 方法 | 信号强度 |
|---|---|---|
| `GET <url>/models` | 模型列表 | 弱（只证明模型发现可用） |
| `OPTIONS <url>/chat/completions` | CORS 预检 | 弱（很多上游不正确实现） |
| `POST <url>/chat/completions` | 最小推理 (`max_tokens=1`) | 强 |
| `OPTIONS <url>/responses` | CORS 预检 | 弱 |
| `POST <url>/responses` | 最小推理 (`store=false`, 小 token) | 强 |
| `OPTIONS <url>/embeddings` | CORS 预检 | 弱 |
| `POST <url>/embeddings` | 最小嵌入 | 强（需用户确认） |

**Anthropic-compatible 候选：**

| 探测 | 方法 | 信号强度 |
|---|---|---|
| `GET <url>/models` | 模型列表 | 弱 |
| `OPTIONS <url>/messages` | CORS 预检 | 弱 |
| `POST <url>/messages` | 最小推理 (`max_tokens=1`) | 强 |

### 6.3 转换能力建议

基于强探测结果给出建议，不直接改配置：

- OpenAI chat 可用 → 可启用 `chat`
- OpenAI responses 可用 → 可启用原生 `responses`
- OpenAI chat 可用 + Anthropic route 需要支持 → 建议 `anthropic_to_chat`
- Anthropic messages 可用 → 可启用 `anthropic`
- Anthropic messages 可用 → 建议 `anthropic_to_responses`（提示无状态限制）
- Embeddings 探测成功 → 可启用 `embeddings`

### 6.4 探测安全规则

- 强探测必须由用户显式触发，避免保存配置时自动消耗额度
- 探测错误需脱敏，不返回 API Key、Authorization、Cookie 或完整 provider headers
- 对桥接能力，精确 probe 走真实请求路径（如 `anthropic_to_chat` 的 anthropic probe 先把 Messages 请求转换成上游 Chat 请求再探测）

---

## 7. 创建体验

Admin 新建 provider 的体验是在配置真相层之上增加一层面向人的创建模型，避免用户在创建阶段直接维护底层 schema 字段之间的耦合关系。

### 7.1 创建流程

1. **选择 Provider 类型**
   - 当前支持：通用 HTTP Provider、Ollama / Chat Only、CLIProxy（Codex/Gemini/Claude）、Copilot
   - 通用 HTTP Provider 默认 `format = "openai"`，国内主流厂商无需额外选择

2. **填写连接信息**
   - 名称、URL、认证来源、proxy、headers
   - 认证来源是显式选择：静态 API Key、命令、无认证（Copilot 额外提供 config_dir）
   - 命令认证在 UI 中标记为受信任 operator-only 配置
   - cliproxy 预设不展示命令认证，由 CLIProxyAPI auth_dir 管理本地 CLI 凭证

3. **声明能力**
   - 默认展示 `format = "openai"` + 全协议（chat、responses、embeddings）
   - 用户可收窄为 chat-only 或按需勾选
   - 桥接开关按 endpoint 展示：anthropic_to_chat（OpenAI format endpoint）、anthropic_to_responses（Anthropic format endpoint）

4. **检测接入方式**（用户显式触发）
   - 页面列出候选协议、模型发现结果、当前可用接口
   - 每个能力有限制说明
   - 对多 endpoint provider（如 Coding Plan），每个 endpoint 独立探测

5. **保存**
   - 保存时按当前 provider 字段归一化有效认证来源
   - 单 endpoint provider 通常只生成 provider 级简写字段，不写 `endpoint.*`
   - 多 endpoint provider 显式写 `endpoint.*`
   - 写回完整、显式的 provider 配置

### 7.2 编辑流程

- 保留连接信息主表单
- 单 endpoint 时 `format` 作为下拉选择，默认 `openai`
- 多 endpoint 时每个 endpoint 独立配置 `url`、`format`、`protocols`
- 桥接开关在 endpoint 级展示
- `endpoint` 作为独立折叠分组：只在需要多 endpoint 时展开
- 每个 endpoint 可独立覆盖 URL、headers、models、protocols
- 静态模型基线在 provider 级编辑
- 探测结果不自动保存

### 7.3 Preset 派生规则

- Provider 类型会派生底层默认值（`format`、`backend`、`backend_provider`、默认 `url`、默认 `config_dir`）
- cliproxy 类型优先复用当前配置里已有的 cliproxy endpoint；没有现成 endpoint 时回退到 `http://127.0.0.1:18741/v1`
- cliproxy 预设固定 `format = "openai"`，默认 chat-only
- 派生值写回现有 `provider.*` schema，不引入新的持久化字段
- Preset 和 capability template 只是输入辅助层，不能成为运行时真相

### 7.4 后端元数据 API

`GET /_admin/api/providers/form-meta` 返回：

- Provider type presets
- Service protocol templates
- cliproxy 默认 endpoint 推导结果

这层元数据属于 admin 体验，放在 `internal/gateway/admin` 而非 `config`。

### 7.5 UI 约束

- 不提供"自定义底层字段"入口，用户只能在 Warden 支持的 format 与能力模板中选择
- 不暴露旧 `family` / `protocol` / provider 级 `service_protocols` 的编辑入口
- 旧配置无法匹配任何 preset 时，页面显示迁移提示
- 不保留 YAML 配置读写路径；配置真相只写回 TOML

---

## 8. 设计决策

### 为什么用 `endpoint` 取代 `access`？

旧 `access` 模型的核心问题是语义为"覆盖"而非"定义"。`access.openai.url` 的含义是"覆盖 provider 级的 URL"，但 Coding Plan 的真实结构是"两个独立的协议入口，共享同一个 API Key"。

`endpoint` 的语义是"定义"：每个 endpoint 是一个完整的协议接入点，有自己的 `format`、`url`、`protocols`。这与 Coding Plan（阿里云百炼、腾讯云、讯飞、火山引擎）"一个套餐 = 一个 Key + 多个协议入口"的真实结构完全一致。

### 为什么 `format` 和 `protocols` 下沉到 endpoint 级？

在旧模型中，`format` 在 provider 级，`access` 的 format 通过子结构名隐式推导（`access.openai` → format=openai）。这导致：

- 无法表达"两个相同 format 的不同 endpoint"（如企业网关的国内/海外两个 OpenAI 入口）
- 用户需要理解"provider 级 openai + access 级 anthropic"这种反直觉组合

下沉到 endpoint 级后，每个 endpoint 自包含，不再依赖 provider 级的隐式默认值。

### 为什么桥接开关也下沉到 endpoint 级？

桥接是"某个 endpoint 是否愿意接收转换后的请求"。旧模型放在 provider 级，隐含假设 provider 只有一个对应 format 的 endpoint。多 endpoint 时无法精确控制（如两个 OpenAI endpoint，一个开桥接一个不开）。

放在 endpoint 级后，控制粒度精确到每个入口。

### 为什么单 endpoint 场景保持简写兼容？

国内 95% 的 provider（Kimi、DeepSeek、GLM 等）只提供单个 OpenAI-compatible 入口。强制它们写 `[provider.xxx.endpoint.default]` 是纯粹的配置噪音。

简写规则允许：`url` + `format` + `protocols` 写在 provider 级时，自动构成一个隐式 `default` endpoint。配置形态与旧模型完全一致，零迁移成本。

### 为什么 provider 级 `url`/`format`/`protocols` 与 `endpoint.*` 互斥？

允许混合写法（如 provider 级写 `url`，endpoint 级再写自己的 `url`）会重新引入"覆盖"语义，让配置层级关系变得模糊。

强制互斥：要么全用简写（provider 级），要么全用显式（endpoint 级）。Validate 报错阻止混合写法。

### 为什么 endpoint 间共享 `api_key`？

Coding Plan 的 OpenAI 入口和 Anthropic 入口使用**同一个 API Key**。如果同一供应商的不同接入方式需要不同凭证，那就是两个独立账户，应该建成两个 provider。

这个约束保持模型清晰：provider = 一个认证账户。

### 为什么探测不自动写入配置？

如果把探测结果自动保存，保存配置就变成有副作用的网络操作——网络波动、上游变更、额度消耗都会意外改变配置真相。探测结果只应作为 UI 建议。

### 为什么不引入 per-route format override？

当前需求是简化 provider 配置，不是给 route 增加更复杂的选择语法。Provider 顺序继续是用户表达优先级的主手段，endpoint 排序只解决同 provider 内部的选择。若需要强制走某个 endpoint，用两个 provider 配置作为过渡方案。

### 为什么展示层和路由层真相分离？

路由必须基于显式配置 + 静态校验，否则启动期网络波动会改变路由行为。展示层的探测提示是辅助性的——它只影响 UI 上的绿色/黄色标记，不影响请求的实际路由结果。

### 为什么不在 `Validate()` 中做 URL 兼容改写？

配置校验不应有网络依赖。URL 的 `/v1` 兼容探测和用户确认属于 UI 流程，不属于配置验证。运行时不在 as-is 与 `/v1` 之间 failover，避免把确定性配置错误伪装成运行时重试。
