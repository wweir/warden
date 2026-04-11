# pkg/toolhook

`pkg/toolhook` 提供与执行器无关的通用 Tool Hook 能力，适用于任意 tool call。

## 职责

- 根据命中的 `route.<prefix>.hooks` 规则匹配 tool 名称（`MatchHooks`）
- 并发执行 `block`/`async` hook（`RunBlock` / `RunAsync`）
- 支持三种 hook 类型：
    - `exec`: 子进程执行，`stdin` 传入 JSON 上下文
    - `ai`: 调用网关 `chat/completions` 做策略判定
    - `http`: 调用配置中的 `webhook` 端点（支持重试与 body_template，模板数据为 `.CallContext` / `.Args`）

## When 模式

- `block`（阻断模式）：非流式请求下，被拒绝的 tool call 会从响应中移除；流式请求降级为 async 行为
- `async`（异步审计）：审查结果会在后台完成后回写到请求日志的 `tool_verdicts` 字段，前端标注不安全的请求
- 旧配置值 `pre`/`post` 在验证时自动规范化为 `block`/`async`

## 上下文结构

Hook 收到的 JSON 为 `CallContext`：

- `tool_name`: 原始工具名（如 `write_file` 或 `weather`）
- `full_name`: 完整工具名（如 `web_search` 或 `filesystem__write_file`）
- `mcp_name`: 当工具名使用 `<prefix>__<name>` 形式时会拆出前缀；当前 route 不再自动注入 MCP 工具
- `call_id`: 本次工具调用 ID
- `arguments`: 工具参数原始 JSON
- `result`: 工具结果（仅 async）
- `is_error`: 工具是否失败（仅 async）

## HookVerdict

`RunBlock` 和 `RunAsync` 返回 `HookVerdict`，包含：

- `ToolName`: 工具全名
- `CallID`: 调用 ID
- `Rejected`: 是否被拒绝
- `Reason`: 拒绝原因
- `Mode`: "block" 或 "async"

## 失败策略

- Hook 运行错误（超时/崩溃/网络）采用 fail-open（放行）
- 仅当 hook 明确返回 `{"allow": false, "reason": "..."}` 时才视为拒绝
- `block` 拒绝会导致 tool call 从非流式响应中移除；`async` 拒绝仅记录日志（审计语义）
- `async` hook 作为异步审计逻辑，会保留 route-scoped context value，但不会跟随下游 request cancellation 一起提前终止；真正的执行上限仍由每个 hook 自己的 timeout 控制
- `ai` hook 使用 `hook.timeout` 作为请求超时；`http` hook 使用 `webhook.timeout`，未配置时默认 `5s`

## 返回格式

`exec` 的 `stdout` 与 `ai` 的模型响应都应返回：

```json
{ "allow": true, "reason": "ok" }
```

或

```json
{ "allow": false, "reason": "blocked because ..." }
```

`http` hook 的 webhook 响应体也使用同样格式；若未返回可解析 JSON，则按 fail-open 处理。
