# Session 级日志去重设计

## 背景

同一 session 的多轮对话会产生多条独立的 HTTP 请求，每条请求都会生成一条 `reqlog.Record`。在长会话场景下：

- 后端 `Broadcaster` 的 50 条环形缓冲很快被同一 session 的多个请求占满
- 前端 `useLogStream` 的 500 条数组上限同样会被长会话快速消耗
- `FileLogger` 每个请求写一个独立文件，磁盘上产生大量历史文件

由于 Chat Completion / Responses 请求体中的 `messages` / `input` 已经携带了**完整的对话历史**，保留中间轮次的旧记录在信息上是冗余的。因此决定引入 session 级去重：同 session 只保留最新一条完整日志。

## Session Key 定义

```
SessionKey = Route + "\x00" + Fingerprint[:6]  (sysHash)
```

- `Fingerprint` 由 `internal/reqlog/fingerprint` 从请求体提取，前 6 位是 system prompt 的哈希（sysHash）
- 同一 session 的多轮请求，消息数组增长导致 fingerprint 变长，但 sysHash 始终相同
- sysHash 为 6 位十六进制（24 bit），碰撞概率约 1/16M，实际可忽略
- fingerprint 为空或长度不足 6 的记录回退到 `request_id` 级去重

## 后端实现

### `internal/reqlog/types.go`

给 `Record` 增加 `SessionKey()` 方法：

```go
func (r Record) SessionKey() string {
    if r.Fingerprint == "" || len(r.Fingerprint) < 6 {
        return ""
    }
    return r.Route + "\x00" + r.Fingerprint[:6]
}
```

不引入新 JSON 字段，协议保持不变。

### `internal/reqlog/broadcast.go`

`Publish` 的去重优先级：

1. `request_id` 相同 → 替换（保留 streaming 的 pending -> final 更新）
2. `request_id` 不同但 `SessionKey` 相同 → 替换（跨轮次去重）
3. 都不匹配 → 写入 ring buffer

效果：50 条 ring buffer 从"保留 50 个请求"变成"保留 50 个 session"。

### `internal/reqlog/file.go`

当记录存在 session key 时，文件名固定为 `{route}_{sysHash}.json`，直接覆盖旧文件。route 只用于文件名展示，路径分隔符会转成 `_`：

```go
if sysHash := r.sessionSysHash(); sysHash != "" {
    filename = route + "_" + sysHash + ".json"
}
```

## 前端实现

### `web/admin/src/composables/useLogStream.js`

引入与后端完全一致的 session key 计算逻辑：

```js
function getSessionKey(log) {
    if (!log.fingerprint || log.fingerprint.length < 6) return null;
    const sysHash = log.fingerprint.slice(0, 6);
    return (log.route || "(unknown)") + "\0" + sysHash;
}
```

`upsertLog` 的行为：

1. `request_id` 已存在 → 直接替换
2. `request_id` 不存在但 `sessionKey` 已存在 → 删除旧记录并替换
3. 都不存在 → 追加新记录

### 页面渲染调整

由于后端 broadcaster 已按 session 去重，前端收到的 `chainedLogs` 中每个 chain 只会有一条日志：

- **列表自动扁平化**：模板中 `chain.displayLogs.length === 1` 分支自动命中，不再显示 chain 展开/折叠交互
- **SessionTreePanel 简化为 route filter**：去掉 session 列表，只保留 route 分组按钮，点击后过滤右侧日志表
- **移除冗余状态**：`expandedChains`、`activeSession`、`collapsedRouteGroups` 等状态不再需要

## Trade-off

| 方面 | 影响 |
|------|------|
| **信息完整性** | 中间轮次的 `response` 被丢弃，但最新请求的 `request.messages` 已包含完整对话历史；对大部分排障场景足够 |
| **sysHash 碰撞** | 不同独立对话若 system prompt 相同可能误判为同一 session（概率 1/16M） |
| **内存占用** | 从 O(请求数) 降到 O(session 数)，长会话场景效果显著 |
| **磁盘占用** | 每个 session 只有一个文件（若启用 file logger） |
| **HTTP 日志外发** | `HTTPLogger` 不改，外部 SIEM 仍能看到所有请求 |

## 相关文件

- `internal/reqlog/types.go`
- `internal/reqlog/broadcast.go`
- `internal/reqlog/file.go`
- `web/admin/src/composables/useLogStream.js`
- `web/admin/src/views/Logs.vue`
- `web/admin/src/components/SessionTreePanel.vue`
