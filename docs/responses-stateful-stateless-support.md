# Responses API 有状态 / 无状态支持现状

本文只整理当前代码事实，不替实现洗地。

## 术语先定死

- 无状态：客户端每次请求都把当前轮需要的上下文放进 `input`，网关不依赖历史 `response.id`
- 有状态：客户端通过 `previous_response_id` 续接上一次 Responses 结果，让上游服务端负责会话状态

这里有一个容易偷换概念的点：

- **warden 不维护本地 Responses 会话状态**
- 所谓“支持有状态”，本质上是 **允许并转发 `previous_response_id` 给上游**
- 因此它不是“网关自己实现了有状态协议”，而是“网关对上游有状态能力做了透传或兼容”

## 结论矩阵

| 场景 | 无状态支持 | 有状态支持 | 结论 |
| --- | --- | --- | --- |
| 客户端请求 `/responses`，上游也走原生 `/responses` | 完整 | 条件完整 | 最可靠路径 |
| 客户端请求 `/responses`，provider 开启 `responses_to_chat`，上游改走 `/chat/completions` | 部分 | 不支持 | 只能当兼容桥接，不能当完整 Responses |
| 管理端 `Logs` 页面会话整合 | 支持展示 | 支持展示 | 仅观测能力，不等于运行时协议能力 |

## 1. 原生 `/responses` 路径

对应代码：

- `internal/gateway/api_responses.go`
- `internal/gateway/adapter.go`
- `pkg/protocol/openai/prompt.go`

行为特点：

1. 请求体默认按原始 JSON 转发，只做少量必要处理：
   - 按 route 选择结果重写 `model`
   - 按 route 配置注入 system prompt
2. 其余字段不会被 Responses 专用结构体先解析再重建，因此 `previous_response_id`、`store` 一类字段不会在网关层被主动剥掉
3. 流式与非流式都支持；流式日志会尝试从 `response.completed` 还原最终对象，便于审计

所以：

- 无状态请求：**完整支持**
- 有状态请求：**协议透传层面支持**

但这个“支持”有前提，不要自欺欺人：

1. **状态存储不在 warden，本质依赖上游 provider**
2. **warden 没有 `response.id -> provider` 亲和性绑定**
3. 带 `previous_response_id` 的请求现在会禁用 failover，避免网关把续接请求切到另一家 provider
4. 但首次带状态的那一轮若未显式固定 provider，仍然取决于当次路由选择结果；状态是否有效依赖该 provider/账号之前是否真正产出了对应 `response.id`

因此原生有状态支持的真实结论不是“绝对完整”，而是：

- **单 provider 或显式固定 `X-Provider` 时，可认为基本可用**
- **多 provider 场景下，网关不会为有状态请求做 failover；续接是否成立仍取决于首选到的 provider 是否持有该状态**

## 2. `responses_to_chat` 桥接路径

对应代码：

- `internal/gateway/api_responses.go` 中 `handleResponsesViaChat`
- `pkg/protocol/openai/convert.go`

这个模式的本质是：

1. 先把 Responses 请求转成 Chat 请求
2. 上游调用 `/chat/completions`
3. 再把 Chat 响应转回 Responses 结构

这条路天然不可能完整等价，代码里已经把限制写死了。

### 2.1 有状态：明确不支持

`pkg/protocol/openai/convert.go` 在 `ResponsesRequestToChatRequest` 中直接拒绝：

- `previous_response_id is not supported in responses_to_chat mode`

这不是“暂时没测”，而是**实现上显式禁止**。

原因很简单：

- Chat Completions 本身没有 `previous_response_id` 语义
- warden 也没有本地状态机去补这个缺口

所以该模式下的有状态支持程度是：**0，明确不支持**

### 2.2 无状态：只支持 Chat 兼容子集

当前可工作的子集：

1. `input` 为字符串
2. `input` 为数组，且主要由 `message` / `function_call` / `function_call_output` / `reasoning` 构成
3. `tools` 为 `function` 类型
4. 流式和非流式都能做基础转换

当前不能按原义保证的部分：

1. 非 `function` tools（如 `web_search`、`file_search`）会被降级成 mock function tool，只是“保留信息”，不是原生能力等价
2. 未知 `input` item type 会被降级成 mock tool call，同样只是兼容兜底，不是语义等价
3. 这意味着某些 Responses 专属能力在桥接后只能“尽量不丢字段”，不能保证上游 Chat 模型真正理解或执行

所以该模式下无状态支持的真实结论是：

- **基础文本 / function tools / tool result 往返可用**
- **Responses 专属能力只算部分支持**

## 3. 已移除的 `chat_to_responses`

`chat_to_responses` 已从当前实现移除。

原因很直接：

1. 它和“客户端直接使用 Responses 协议”的支持程度是两回事
2. 它会把边界搞乱，让人误以为 gateway 已经具备完整的 Responses 状态语义
3. 当前保留的目标只有一个：把**无状态** Responses 兼容桥接到 Chat，上行有状态请求则只允许原生透传

## 4. 管理端 Logs 页面

对应代码：

- `web/admin/src/views/Logs.vue`
- `ARCHITECTURE.md`
- `web/admin/README.md`

这里分两层：

1. 请求内容展示
   - 能展开无状态 `input`
   - 能显示 `function_call` / `function_call_output`
2. 会话链整合
   - 优先按 `previous_response_id -> response.id` 显式关联
   - 没有关联时再退回 fingerprint 和旧启发式

因此管理端对两种模式的**观测支持**是够的。

但不要混淆：

- 这只是日志整合能力
- 它不提供运行时续接，也不修复跨 provider 的状态一致性问题

## 5. 最终判断

### 可以放心说“支持”的部分

1. 原生 `/responses` 无状态请求
2. 原生 `/responses` 在同一上游 provider 上的有状态透传
3. Responses 流式/非流式日志落盘与管理端展示

### 只能说“部分支持”的部分

1. `responses_to_chat` 下的无状态兼容
2. 原生 `/responses` 在多 provider 自动切换场景下的有状态续接

### 不能说“支持”的部分

1. `responses_to_chat` 下的 `previous_response_id`
2. 任何需要 warden 自己维护 Responses 会话状态的能力
3. 把 Responses 专属 tools 在 Chat 上游中当成原生等价能力
