# docs

`docs/` 只放当前仍有独立维护价值的专题文档。

这里不应该变成方案墓地。已经失效、只剩历史意义、且结论已被根文档吸收的文档，应直接删除。

文档优先级：

1. [README.md](/home/wweir/Mine/warden/README.md)：项目入口、能力概览、构建运行
2. [ARCHITECTURE.md](/home/wweir/Mine/warden/ARCHITECTURE.md)：系统边界、分层职责、关键数据流
3. [config/README.md](/home/wweir/Mine/warden/config/README.md)：配置模型与校验规则
4. `docs/*.md`：仅在根文档无法低噪声覆盖时，补充专题边界

## Topics

- [responses-stateful-stateless-support.md](/home/wweir/Mine/warden/docs/responses-stateful-stateless-support.md)
  Responses API 的有状态/无状态支持边界，以及 `responses_to_chat` 的限制
- [provider-dynamic-capability-discovery-plan.md](/home/wweir/Mine/warden/docs/provider-dynamic-capability-discovery-plan.md)
  provider 协议能力展示、单协议 route 设计和运行时真相来源
- [anthropic-messages-to-chat-plan.md](/home/wweir/Mine/warden/docs/anthropic-messages-to-chat-plan.md)
  `anthropic_to_chat` 的受控桥接范围
- [api-key-design.md](/home/wweir/Mine/warden/docs/api-key-design.md)
  客户端 API Key 管理和敏感字段编码边界

## Rules

- 变更现状时，先判断应该更新根文档还是专题文档，不要机械地到处复制
- 如果一个主题已经能被 `README.md`、`ARCHITECTURE.md` 或包级 `README.md` 清楚表达，就不要再新增 `docs/*.md`
- 新增专题文档时，在这里登记它解决的具体问题
- 专题文档失效后，如果只剩历史记录价值，直接删除，不做“归档保留”
