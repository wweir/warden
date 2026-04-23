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
   - 典型类型包括 OpenAI 官方、Anthropic 官方、OpenAI-compatible、自定义 Ollama/本地兼容端点、CLIProxy Codex/Gemini/Claude、Qwen、Copilot

2. Derived Base Config
   - provider 类型会派生底层默认值，例如 `family`、`backend`、`backend_provider`、默认 `url`、默认 `config_dir`
   - `cliproxy` 类型会优先复用当前配置里已有的 cliproxy endpoint；没有现成 endpoint 时回退到 `http://127.0.0.1:18741/v1`
   - 派生值仍然写回现有 `provider.*` schema，不引入新的持久化字段

3. Sectioned Form
   - 创建页按“基本信息 / 连接信息 / 认证信息 / 能力信息 / 高级字段”分组
   - 普通用户先完成类型、连接和认证即可；高级 schema 字段折叠到高级区

4. Capability Templates
   - `service_protocols` 不再默认以自由 tag 输入作为主入口
   - 创建页先提供能力模板，例如 adapter defaults、chat only、chat + embeddings、chat + responses + embeddings、anthropic bridge
   - 原始 `service_protocols`、`responses_to_chat`、`anthropic_to_chat` 仍然保留在高级区，供需要精细控制的用户直接修改

## Backend Metadata

admin 后端新增只读元数据接口 `GET /_admin/api/providers/form-meta`，返回：

- provider type presets
- service protocol templates
- cliproxy 默认 endpoint 推导结果

这层元数据属于 admin 体验，不属于配置真相，因此放在 `internal/gateway/admin`，而不是 `config`。

## Non-Goals

- 不修改 `config.ProviderConfig` 持久化结构
- 不引入新的 YAML 顶层字段，例如 `provider.kind` 或 `provider.template`
- 不让后端在保存时静默猜测用户意图；最终写回的仍然是完整、显式的 provider 配置

## Compatibility Boundary

- 旧 YAML 继续可读可写
- 旧 provider 编辑页继续支持直接编辑原始字段
- 新建页的 preset 和 capability template 只是输入辅助层，不能成为运行时真相
