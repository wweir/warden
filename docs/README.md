# docs

`docs/` 只放当前仍有独立维护价值的专题文档。

这里不应该变成方案墓地。已经失效、只剩历史意义、且结论已被根文档吸收的文档，应直接删除。

## Document Hierarchy

### Level 1: Entry Points

1. [README.md](../README.md)：项目入口、能力概览、构建运行
2. [ARCHITECTURE.md](../ARCHITECTURE.md)：系统边界、分层职责、关键数据流
3. [OPERATIONS.md](../OPERATIONS.md)：部署配置、运维规范、平台差异
4. [config/README.md](../config/README.md)：配置模型与校验规则

### Level 2: Topic Guides

- [cross-platform-deployment.md](./cross-platform-deployment.md)
  Linux / macOS / Windows 的运行、托管与运维边界
- [responses-protocol-support.md](./responses-protocol-support.md)
  `responses` 协议的实现边界
- [provider.md](./provider.md)
  provider 子系统配置模型、接入方式和探测规则
- [anthropic-messages-to-chat-plan.md](./anthropic-messages-to-chat-plan.md)
  `anthropic_to_chat` 的桥接边界
- [api-key-design.md](./api-key-design.md)
  客户端 API Key 管理和敏感字段编码边界
- [token-usage-observability.md](./token-usage-observability.md)
  token usage 观测与 metrics 暴露面
- [embeddings-support.md](./embeddings-support.md)
  `/embeddings` 的 service protocol 暴露面
- [cliproxy-backend.md](./cliproxy-backend.md)
  通过 OpenAI-compatible sidecar 接入 CLIProxyAPI/cliproxy 的边界

## Rules

- 变更现状时，先判断应该更新根文档还是专题文档
- 如果一个主题已经能被根文档清楚表达，就不要再新增 `docs/*.md`
- 某些文件名里保留了历史上的 `plan` 字样，但正文必须只描述当前现状
- 专题文档失效后，如果只剩历史记录价值，直接删除

## Review Records

- [docs/reviews/](./reviews/)：审查记录、问题清单和阶段复盘
