# web/admin

## Responsibilities

`web/admin` 是嵌入式 Vue 3 管理端，负责展示和编辑当前后端真实支持的能力：

- Dashboard：provider 状态、路由概览、实时指标
- Providers / Routes：详情与配置编辑
- Tool Hooks：route-scoped hook 编辑与建议
- Logs：SSE 请求日志流
- Logs 流按 `request_id` 合并事件；流式请求会先显示 pending，再在同一条记录上补全最终响应
- Logs 页面整合会话时，支持 Responses API 的无状态输入展开和有状态 `previous_response_id` 续接两种模式
- Config：结构化配置编辑、客户端 API 密钥、验证、应用

约束：

- 不展示已移除的 MCP / SSH 运行时能力
- Tool Hooks 中的 `mcp_name` / `MCPName` 只是工具名命名空间拆分结果，页面文案按 namespace 展示
- 前端只消费当前 admin API，不依赖隐式本地状态

当前配置页中的 route 编辑约束：

- `Routes` 页面负责 route 配置编辑，显式区分 `exact_models` 和 `wildcard_models`
- `Routes` 页面中的 exact model upstream 与 wildcard provider 都以列表顺序表达优先级，并支持在 UI 中调整前后顺序
- `Routes` 页面中的 exact upstream model 建议值会合并 `provider.models` 静态配置与运行时 `/models` 已发现结果，减少手工抄写
- `Routes` 页面中的模型编辑卡片使用左右分栏：左侧维护公开模型信息，右侧维护 upstream/provider 优先级，减少模型较多时的纵向长度
- route model 的额外 system prompt 采用渐进披露：默认关闭，只有显式启用后才展示输入框；前端保存 `prompt_enabled`，后端也只在开关开启时注入该 prompt
- `Providers` 页面卡片支持直接跳到“基于该 provider 模型创建 route”的新建入口；新 route 会默认生成该 provider 已配置/已发现模型的 `exact_models`，并把每个 public model 绑定回同名 upstream model
- `Tool Hooks` 页面负责 route hooks 编辑
- `Providers` 页面负责单个 provider 配置编辑；`provider.models` 在 UI 中被明确当作“静态模型基线/兜底”，并复用运行时已发现模型作为录入建议，不等同于 route 对外暴露模型定义
- `Config` 页面保留通用配置、客户端 API 密钥、webhook 和日志目标

## Dashboard Data Flow

Dashboard 消费 `GET /_admin/api/status` 与 `GET /_admin/api/metrics/stream`：

- `status` 提供 provider 与 route 概览
- `metrics/stream` 提供聚合指标和滚动时序点
- `RealtimeLineChart.vue` 使用 uPlot 渲染实时曲线，并共享时间窗口与 hover 轴

## Build

- 开发：`npm run dev`
- 生产构建：`npm run build`
