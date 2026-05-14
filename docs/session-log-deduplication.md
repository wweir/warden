# Session 级日志去重设计

## 背景

同一会话的多轮请求通常会在新请求体里携带完整历史，因此旧请求在实时日志里会重复占用空间。但 Warden 当前没有可靠的服务端 session id，不能只靠 system prompt、agent 前缀或 fingerprint 前缀判断同一会话。

## 判定规则

去重只允许两种情况：

1. `request_id` 相同：同一个流式请求的 pending 记录被 final 记录替换。
2. `request_id` 不同，但新请求的归一化完整对话文本包含旧请求的归一化完整对话文本，且新文本更长：认为是同一会话的后续轮次，替换旧记录。

禁止使用 `Fingerprint[:6]`、system prompt hash、agent 文本前缀或相同 route 作为跨请求去重依据。两个并发独立 session 即使 agent 前缀完全相同，也必须保留为两条记录。

## 后端实现

- `internal/reqlog/fingerprint.ConversationText` 从 Chat Completions `messages[]` 和 Responses `input[]` 中提取完整归一化对话文本。
- `reqlog.Record.Continues(prev)` 校验 route 相同、request id 不同、当前完整文本包含旧完整文本且更长。
- `Broadcaster.Publish` 先按 `request_id` 覆盖，再按 `Continues` 覆盖，否则写入 ring buffer。
- `FileLogger` 保持每个 request 一个文件，文件名包含 `request_id`，不再按 session hash 覆盖。

## 前端实现

- `useLogStream` 与后端一致：先按 `request_id` 覆盖，再用完整对话包含关系合并后续轮次。
- 侧栏 session 项使用 `request_id` 作为选择 key，避免相同 fingerprint 或相同 agent 前缀的并发请求互相覆盖。

## Trade-off

| 方面 | 影响 |
|------|------|
| 信息完整性 | 并发独立会话不会被误合并；真正的后续轮次仍会折叠到最新完整请求 |
| 内存占用 | 不再把相同 agent 前缀的并发 session 压成一条，记录数可能比旧策略更多，但语义正确 |
| 磁盘占用 | 文件日志回到 request 粒度，避免覆盖并发请求 |
| 未来改进 | 若上游或客户端提供稳定 session id，应优先使用显式 session id，而不是内容推断 |

## 相关文件

- `internal/reqlog/fingerprint/fingerprint.go`
- `internal/reqlog/types.go`
- `internal/reqlog/broadcast.go`
- `internal/reqlog/file.go`
- `web/admin/src/composables/useLogStream.js`
- `web/admin/src/views/Logs.vue`
