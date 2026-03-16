# pkg/toolhook

`pkg/toolhook` 提供与执行器无关的通用 Tool Hook 能力，适用于任意 tool call。

## 职责

- 根据命中的 `route.<prefix>.hooks` 规则匹配 tool 名称（`MatchHooks`）
- 并发执行 `pre`/`post` hook（`RunPre` / `RunPost`）
- 支持三种 hook 类型：
    - `exec`: 子进程执行，`stdin` 传入 JSON 上下文
    - `ai`: 调用网关 `chat/completions` 做策略判定
    - `http`: 调用配置中的 `webhook` 端点（支持重试与 body_template，模板数据为 `.CallContext` / `.Args`）

## 上下文结构

Hook 收到的 JSON 为 `CallContext`：

- `tool_name`: 原始工具名（如 `write_file` 或 `weather`）
- `full_name`: 完整工具名（如注入 MCP 工具 `filesystem__write_file`）
- `mcp_name`: MCP 名称（仅注入 MCP 工具时有值）
- `call_id`: 本次工具调用 ID
- `arguments`: 工具参数原始 JSON
- `result`: 工具结果（仅 post）
- `is_error`: 工具是否失败（仅 post）

## 失败策略

- Hook 运行错误（超时/崩溃/网络）采用 fail-open（放行）
- 仅当 hook 明确返回 `{"allow": false, "reason": "..."}` 时才视为拒绝
- `pre` 拒绝会返回错误；`post` 拒绝仅记录日志（审计语义）
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
