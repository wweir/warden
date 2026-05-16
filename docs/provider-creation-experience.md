# Provider Creation Experience

`provider.*` 仍然是 Warden 的配置真相。admin 新建 provider 体验只是在这个真相层之上增加一层面向人的创建模型，避免用户在创建阶段直接维护底层 schema 字段之间的耦合关系。

## Problem

旧的 provider 新建页基本等于 `config.ProviderConfig` 的原始字段平铺：

- 用户需要自己理解 `family`、`backend`、`backend_provider`、`service_protocols`、`api_key`、`config_dir`、`url` 之间的依赖
- `cliproxy` 场景下需要手工维护 `family: openai`、`backend: cliproxy`、`backend_provider`、显式 `service_protocols` 和共享 loopback `url`
- `service_protocols` 是能力覆盖字段，但旧 UI 把它作为自由输入字段直接暴露，用户很难判断什么是推荐值，什么是危险覆盖

这导致的问题不是 schema 冗余，而是创建交互把内部配置约束直接甩给用户。

## Current Design

当前实现把 provider 新建体验拆成四层：

1. Provider Type
   - 新建时先选 provider 类型，而不是先填底层字段
   - 当前页面只提供 Warden 明确支持的 provider presets：OpenAI-compatible、Anthropic-compatible、Ollama / Chat Only、CLIProxy Codex/Gemini/Claude、Copilot
   - 页面不提供自定义 provider type；OpenAI-compatible / Anthropic-compatible 只表示同族协议兼容端点，不能打开底层字段自由编辑入口

2. Derived Base Config
   - provider 类型会派生底层默认值，例如 `family`、`backend`、`backend_provider`、默认 `url`、默认 `config_dir`
   - `cliproxy` 类型会优先复用当前配置里已有的 cliproxy endpoint；没有现成 endpoint 时回退到 `http://127.0.0.1:18741/v1`
   - `cliproxy` 的 URL 是 Warden 到 CLIProxyAPI 服务的内部 HTTP 边界；普通预设路径隐藏该字段
   - `cliproxy` 的连接说明只描述本地/内嵌 endpoint 托管；认证说明只描述 CLIProxyAPI `auth_dir` 中的本地 CLI 登录凭证，避免把 endpoint 和 API Key 混在一起。provider 详情页提供独立的认证导入面板，只把完整 CLIProxyAPI auth JSON 写入 `auth_dir`，不写回 provider 配置。认证导入只做离线结构校验和状态提示，不在导入路径中刷新 token 或访问上游；在线验证必须由用户手动触发，并由后端沿当前 cliproxy provider 的正常请求探测链路发出
   - 派生值仍然写回现有 `provider.*` schema，不引入新的持久化字段
   - `family`、`backend`、`backend_provider` 不作为接入类型选项出现；它们作为底层配置真相保留，但 provider 详情页不提供编辑入口。当前字段无法匹配任何 preset 时，页面只显示被动提示。

3. Common Config First
   - 创建页把接入类型、名称、URL、认证来源和可用接口收敛到一个常用配置区
   - 认证来源是显式选择：静态 API Key、命令、无认证；Copilot 额外提供配置目录。每种认证来源的具体字段内联在该选择器下，切换来源时只展示当前来源需要的认证信息。命令认证只写回 `api_key_command` / timeout / TTL，不引入新的 provider type，也不改变 provider family 或可用协议。
   - 保存时必须按当前 provider 字段重新归一化有效认证来源；例如字段切换为 `backend: cliproxy` 后，认证来源必须落到 CLIProxyAPI `auth_dir` 的无 provider API key 模式，不能继续使用切换前残留的静态 API Key 或命令模式。
   - 命令认证在 UI 中标记为受信任 operator-only 配置，因为它会以 Warden 服务用户身份执行 shell 命令；cliproxy 托管预设不展示命令认证，仍由 CLIProxyAPI auth_dir 管理本地 CLI 凭证。
   - 静态模型基线直接展示，避免隐藏可保存配置项；运行时诊断仍然独立于保存配置的主表单
   - 普通用户先完成常用配置即可；页面不再提供底层字段手动覆盖区

4. Capability Templates
   - 常用区不直接暴露 `service_protocols` 作为主入口，而是用“可用接口”描述用户真正关心的能力面
   - “可用接口”选项只来自 Warden 内置 capability templates，包括仅聊天、聊天 + 向量、聊天 + Responses + 向量、聊天 + Anthropic Messages、Anthropic Messages 兼容、Responses 走 Anthropic Messages
   - 页面用只读徽标展示最终可用接口，例如 Chat、Responses、Embeddings、Anthropic Messages
   - 页面不提供“自定义接口”入口；原始 `service_protocols`、`responses_to_chat`、`anthropic_to_chat` 不在 provider 详情页直接编辑
   - 当前配置无法匹配任一内置接口组合时，页面只提示选择 Warden 明确支持的组合，不提供自由兜底编辑；保存前必须重新选择内置接口组合
   - `cliproxy` 预设默认只选择“仅聊天”，避免把 CLIProxyAPI 的原生 Claude/Gemini/Responses 路由误认为 Warden 已经全部一等接入

## Backend Metadata

admin 后端新增只读元数据接口 `GET /_admin/api/providers/form-meta`，返回：

- provider type presets
- service protocol templates
- cliproxy 默认 endpoint 推导结果

这层元数据属于 admin 体验，不属于配置真相，因此放在 `internal/gateway/admin`，而不是 `config`。

## Non-Goals

- 不引入新的 provider family/protocol 或 `provider.kind`；命令认证是鉴权来源字段，不是 provider 类型
- 不引入新的 TOML 顶层字段，例如 `provider.kind` 或 `provider.template`
- 不让后端在保存时静默猜测用户意图；最终写回的仍然是完整、显式的 provider 配置

## Compatibility Boundary

- 不保留 YAML 配置读写路径；配置真相只写回 TOML。
- provider 详情页不再支持直接编辑底层 adapter 字段或原始接口字段；旧配置若无法匹配内置 preset / capability template，只能先选择 Warden 支持的组合再保存
- 新建页的 preset 和 capability template 只是输入辅助层，不能成为运行时真相
