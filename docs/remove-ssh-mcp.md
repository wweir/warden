# Remove SSH and MCP Runtime

## Status

- Start: 2026-03-14
- End: 2026-03-14
- State: completed

## Goal

收缩 Warden 的职责边界，移除以下能力：

- 内置 MCP client 运行时
- SSH 远程执行与 SSH 配置块
- 管理端中对应的状态、详情、调试和编辑入口

## Decisions

1. 不保留兼容空壳字段；直接从配置模型中删除 `mcp` / `ssh`
2. Provider OAuth 凭证读取收敛为本地文件，不再保留远程读取分支
3. 管理端状态流、路由、API 和页面同步删掉 MCP/SSH 入口
4. 架构文档与 README 以当前实现为准，不再把旧能力描述成现行能力

## Impact

- 配置文件中已有的 `mcp` / `ssh` 块将不再被识别
- `/_admin/api/mcp/*` 接口移除
- 管理端不再展示 MCP / SSH 页面或配置段
- `pkg/provider` 的 OAuth 读取接口简化为本地模式
