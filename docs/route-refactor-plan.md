# Route Refactor Archive

> 归档日期：2026-03-19

旧的 route 重构方案已经失效。

失效原因：

- 不再使用 route-level `route.protocols`
- 也不再使用 model-level `protocols` 子块
- 当前实现改为 `route.protocol` 锁定唯一协议

当前真实结构请看：

- `docs/provider-dynamic-capability-discovery-plan.md`
- `ARCHITECTURE.md`
- `config/warden.example.yaml`
