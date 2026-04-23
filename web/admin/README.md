# web/admin

## Responsibilities

`web/admin` 是嵌入式 Vue 3 管理端，负责展示和编辑当前后端真实支持的能力：

- Dashboard：provider 状态、路由概览、实时指标
- Providers / Routes：详情与配置编辑
- Tool Hooks：route-scoped hook 编辑与建议
- Logs：SSE 请求日志流
- Logs 流按 `request_id` 合并事件；流式请求会先显示 pending，再在同一条记录上补全最终响应
- Logs 页面整合会话时，优先按 Responses API 的 `previous_response_id -> response.id` 显式续接整合；没有显式续接时，只在同 route 下按 fingerprint 前缀做保守归并，不再使用旧的 prompt 哈希 + 时间窗启发式，避免把独立请求误并成同一 session
- Logs 页面桌面端采用“左侧 session 树 + 右侧日志表”的主从布局；顶部动作区单独成组，右侧在表格上方增加 scope 摘要条，显式展示当前 route / session / 时间范围 / 请求数，让左侧选择和右侧明细始终对齐；左侧树支持整栏收起、按 route 分组折叠，并限制在视口内滚动，避免长会话把页面纵向撑长；移动端切换为纵向卡片视图；详情弹层拆分摘要、会话过程和响应结果三段，减少排障时的信息竞争
- Chat：根据 `route.protocol` 自动选择 `/chat/completions`、`/responses` 或 `/messages` 发起请求，并按对应 SSE 格式解析文本输出；对 `responses_stateful` 会本地保存上一轮 `response.id` 并续传 `previous_response_id`
- Config：结构化配置编辑、客户端 API 密钥、验证、应用

约束：

- 不展示已移除的 MCP / SSH 运行时能力
- Tool Hooks 中的 `mcp_name` / `MCPName` 只是工具名命名空间拆分结果，页面文案按 namespace 展示
- 前端只消费当前 admin API，不依赖隐式本地状态

当前配置页中的 route 编辑约束：

- `Routes` 页面负责 route 配置编辑，显式区分 `exact_models` 和 `wildcard_models`
- `Routes` 页面必须先锁定 route 唯一协议；exact model / wildcard model 都直接受这个协议约束
- exact model upstream 与 wildcard provider 都以列表顺序表达优先级，并支持在 UI 中调整前后顺序
- `Routes` 页面中的 exact upstream model 建议值会合并 `provider.models` 静态配置与运行时 `/models` 已发现结果，减少手工抄写
- route 已不再有 route-level `protocols`，也不再允许 model-level `protocols`
- provider 详情页同时展示 `candidate_protocols`、`configured_protocols`、`display_protocols` 和精确 model-level probe 结果
- `Routes` 页面中的模型编辑卡片使用左右分栏：左侧维护公开模型信息，右侧维护 upstream/provider 优先级，减少模型较多时的纵向长度
- `Routes` 页面中的 exact model 卡片把 prompt 开关收敛到公开模型名旁边，并在开启时再展开提示词输入框；upstream 行里给上游模型输入保留更大的横向空间
- `Routes` 页面中的 exact upstream 行默认采用单行紧凑布局：优先级、provider、上游模型和操作并排显示，只在窄屏下折行
- `Routes` 详情页采用“顶部概览 + 左侧主列 + 右侧摘要轨”的高密度布局：顶部显示聚合运行态，左侧先给 exact model 摘要再进入编辑器，右侧承接 provider 运行卡片
- `Routes` 详情页上半区的 exact / wildcard 摘要表镜像当前可编辑配置；exact model 明细行提供“编辑 / 删除”动作，便于对已配置模型继续维护
- `Routes` 页面的 provider 运行态不再保留底部独立大表，而是拆入顶部概览和右侧摘要轨，减少监控数据来回对照
- `Routes` 详情页中的明细表在窄屏下通过横向滚动容器保持可读；自定义模型输入和 provider tag 输入都支持键盘导航与基础 ARIA 语义
- route model 的额外 system prompt 采用渐进披露：默认关闭，只有显式启用后才展示输入框；前端保存 `prompt_enabled`，后端也只在开关开启时注入该 prompt
- `Providers` 页面支持轻量协议检测按钮，以及按 `provider + model + protocol` 的精确探测
- `Providers` 页面卡片支持直接跳到“基于该 provider 模型创建 route”的新建入口；新 route 会默认生成该 provider 已配置/已发现模型的 `exact_models`，并预选单个协议
- `Tool Hooks` 页面负责 route hooks 编辑
- `Providers` 页面负责单个 provider 配置编辑；`provider.models` 在 UI 中被明确当作“静态模型基线/兜底”，并复用运行时已发现模型作为录入建议，不等同于 route 对外暴露模型定义
- provider 编辑器把 `family` 作为推荐字段，并暴露当前真实支持的 provider 配置项：`url`、`api_key`、`config_dir`、`proxy`、`headers`、`models`；对 `openai` provider 额外暴露 `backend` / `backend_provider` 元数据和 `responses_to_chat` / `anthropic_to_chat` 桥接开关
- `Config` 页面保留通用配置、客户端 API 密钥、webhook 和日志目标

## Dashboard Data Flow

Dashboard 消费 `GET /_admin/api/status` 与 `GET /_admin/api/metrics/stream`：

- `status` 提供 provider 与 route 概览
- provider 概览包含 `protocol`、`candidate_protocols` 与 `supported_protocols`，供 Providers 列表卡片展示和搜索
- `metrics/stream` 提供聚合指标和滚动时序点
- `RealtimeLineChart.vue` 使用 uPlot 渲染实时曲线，并共享时间窗口与 hover 轴

## Build

- 安装依赖：`bun install --frozen-lockfile`
- 开发：`bun run dev`
- 生产构建：`bun run build`
