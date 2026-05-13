# web/admin

## Responsibilities

`web/admin` 是嵌入式 Vue 3 管理端，负责展示和编辑当前后端真实支持的能力：

- Dashboard：provider 状态、路由概览、实时指标
- Providers / Routes：详情与配置编辑
- Tool Hooks：route-scoped hook 编辑与建议
- Logs：SSE 请求日志流
- Logs 流按 `request_id` 合并事件；流式请求会先显示 pending，再在同一条记录上补全最终响应
- **Session 级去重**：前端本地数组同样按 `Route + Fingerprint[:6]` 做 session key 去重，同 session 的旧记录被新完整记录替换；这保证长会话不会撑满 500 条日志上限
- Logs 详情弹层展示 `ttft_ms`，有流式首 token 数据时与总耗时并列显示；非流式请求不会伪造该字段
- Logs 页面整合会话时，优先按 Responses API 的 `previous_response_id -> response.id` 显式续接整合；没有显式续接时，只在同 route 下按 fingerprint 前缀做保守归并，不再使用旧的 prompt 哈希 + 时间窗启发式，避免把独立请求误并成同一 session
- Logs 页面桌面端采用"左侧 route 过滤器 + 右侧日志表"的主从布局；顶部动作区单独成组，右侧表格上方只有一行简洁的统计条（当前 route / 时间范围 / 请求数）；后端 session 去重后每条记录自然平铺，前端不再维护 chain 展开/折叠状态，已删除 `useSessionChaining` composable；状态指示改用 pill badge 替代整行背景色；左侧过滤器支持整栏收起；移动端切换为纵向卡片视图；详情弹层拆分摘要、会话过程和响应结果三段
- Chat：根据 `route.protocol` 自动选择 `/chat/completions`、`/responses` 或 `/messages` 发起请求，并按对应 SSE 格式解析文本输出；发起 Responses 请求时显式携带 `store=false`；对 `responses` 协议会本地保存上一轮 `response.id` 并在下一轮带上 `previous_response_id`，由网关透明转发到上游
- Provider 详情页的 cliproxy 认证导入面板只写 `cliproxy.auth_dir` 下的 auth JSON 文件，不回填 provider 配置字段；导入和列表状态只做离线结构校验，不证明账号在线可用；页面会异步读取每个 auth 文件中可展示的用量状态，并优先直接展示计划、认证状态、5 小时限额、周限额和重置时间，后端只返回脱敏后的 quota cooldown、model state、selector 最近记录的 cliproxy 运行态错误响应和白名单用量字段；在线验证按钮只调用 Warden 后端，由后端沿当前 cliproxy provider 的正常 Responses 探测链路发起请求
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
- `Routes` 详情页采用“顶部概览 + 左侧主列 + 右侧摘要轨”的高密度布局：顶部显示聚合运行态，左侧主列先单列依次展示 exact / wildcard model 摘要再进入编辑器，右侧承接 provider 运行卡片
- `Routes` 详情页上半区的 exact / wildcard 摘要表镜像当前可编辑配置，两个摘要区一排一个避免横向拥挤；exact model 明细行提供“编辑 / 删除”动作，wildcard model 明细行展示已发现或已观测的具体模型名，并用固定高度 chip 区域避免长模型列表撑高页面
- `Routes` 页面的 provider 运行态不再保留底部独立大表，而是拆入顶部概览和右侧摘要轨，减少监控数据来回对照
- `Routes` 详情页中的明细表在窄屏下通过横向滚动容器保持可读；自定义模型输入和 provider tag 输入都支持键盘导航与基础 ARIA 语义
- route model 的额外 system prompt 采用渐进披露：默认关闭，只有显式启用后才展示输入框；前端保存 `prompt_enabled`，后端也只在开关开启时注入该 prompt
- `Providers` 页面支持轻量协议检测按钮，以及按 `provider + model + protocol` 的精确探测
- `Providers` 页面卡片支持直接跳到“基于该 provider 模型创建 route”的新建入口；新 route 会默认生成该 provider 已配置/已发现模型的 `exact_models`，并预选单个协议
- `Tool Hooks` 页面负责 route hooks 编辑
- `Providers` 页面负责单个 provider 配置编辑；`provider.models` 在 UI 中被明确当作“静态模型基线/兜底”，直接展示在配置表单中，并复用运行时已发现模型作为录入建议，不等同于 route 对外暴露模型定义
- provider 创建页采用 intent-first 结构：先选 provider type，再填写连接 / 认证 / 能力；认证配置内联在“认证来源”选择器下，选中静态 API Key、命令、配置目录或无认证时只展示该来源需要的字段；静态模型基线和高级字段直接展示，底层 `family`、`backend`、`backend_provider` 只在自定义接入中出现，原始 `service_protocols` 只在自定义接口中出现
- provider 创建页消费后端 `/_admin/api/providers/form-meta` 元数据接口，使用 provider presets 和 capability templates 派生默认值，但最终仍写回现有 `provider.*` schema
- provider 详情页的配置规范化、协议能力推导、保存前清理和重启轮询应复用 `src/config-utils.js` / `src/runtime-utils.js` 等公共 helper；页面组件只负责装配视图和当前表单状态，避免复制后端配置能力规则
- provider 编辑器仍允许直接编辑当前真实支持的 provider 配置项：`url`、`api_key`、`config_dir`、`proxy`、`headers`、`models`；对 `openai` provider 额外暴露 `backend` / `backend_provider` 元数据和 `responses_to_chat` / `anthropic_to_chat` 桥接开关
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

## E2E 测试

使用 Bun v1.3.12+ 内置的 `Bun.WebView` 进行无头浏览器截图验证，无需安装额外 npm 包。支持 WebKit（macOS 零依赖）和 Chrome（跨平台，需系统已安装 Chromium）两种后端。

```bash
bun e2e-bun-webview.ts
```

示例脚本 `e2e-bun-webview.ts`：

```typescript
const ADMIN_PASSWORD = process.env.WARDEN_ADMIN_PASSWORD || "admin";
const BASE_URL = `http://admin:${ADMIN_PASSWORD}@localhost:9832`;

await using view = new Bun.WebView({ width: 1440, height: 900 });

await view.navigate(`${BASE_URL}/_admin/logs`);

// Wait for Vue to mount
for (let i = 0; i < 30; i++) {
	await Bun.sleep(500);
	const mounted = await view.evaluate(
		`document.querySelectorAll('button, table, .panel').length > 0`
	);
	if (mounted) break;
}

await Bun.sleep(3000);

// Screenshot: logs list
await Bun.write("warden-logs.png", await view.screenshot({ format: "png" }));

// Click first "View" button via evaluate
const clicked = await view.evaluate(`(() => {
	const btn = Array.from(document.querySelectorAll('button'))
		.find(b => b.textContent.trim() === 'View');
	if (btn) { btn.click(); return true; }
	return false;
})()`);

if (clicked) {
	await Bun.sleep(1000);
	await Bun.write("warden-log-detail.png", await view.screenshot({ format: "png" }));
}
```

测试前需确保本地 warden 服务已启动（`make install` 或前台运行 `warden`），admin 密码与脚本中的凭据一致。
