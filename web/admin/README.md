# web/admin

## Responsibilities

`web/admin` 是嵌入式 Vue 3 管理端，负责展示和编辑当前后端真实支持的能力：

- Dashboard：provider 状态、路由概览、实时指标
- Providers / Routes：详情、调试与配置编辑
- Tool Hooks：route-scoped hook 编辑与建议
- Logs：SSE 请求日志流
- Config：结构化配置编辑、客户端 API 密钥、验证、应用

约束：

- 不展示已移除的 MCP / SSH 运行时能力
- Tool Hooks 中的 `mcp_name` / `MCPName` 只是工具名命名空间拆分结果，不代表系统仍有 MCP client
- 前端只消费当前 admin API，不依赖隐式本地状态

当前配置页中的 route 编辑约束：

- `Routes` 页面负责 route 配置编辑，显式区分 `exact_models` 和 `wildcard_models`
- `Tool Hooks` 页面负责 route hooks 编辑
- `Providers` 页面负责单个 provider 配置编辑
- `Config` 页面保留通用配置、客户端 API 密钥、webhook 和日志目标

## Dashboard Data Flow

Dashboard 消费 `GET /_admin/api/status` 与 `GET /_admin/api/metrics/stream`：

- `status` 提供 provider 与 route 概览
- `metrics/stream` 提供聚合指标和滚动时序点
- `RealtimeLineChart.vue` 使用 uPlot 渲染实时曲线，并共享时间窗口与 hover 轴

## Build

- 开发：`npm run dev`
- 生产构建：`npm run build`
