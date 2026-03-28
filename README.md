# Warden

Warden 是一个面向多上游 LLM 的轻量级 AI Gateway。它暴露统一的 OpenAI 兼容入口，把请求按路由和模型映射到不同 provider，并在请求链路上提供有限协议兼容、System Prompt 注入、工具调用 Hook、日志与管理面板。

文档入口：

- 架构总览：[ARCHITECTURE.md](/home/wweir/Mine/warden/ARCHITECTURE.md)
- 专题文档索引：[docs/README.md](/home/wweir/Mine/warden/docs/README.md)
- 配置层说明：[config/README.md](/home/wweir/Mine/warden/config/README.md)

## 当前能力

- 统一接入 `openai`、`anthropic`、`ollama`、`qwen`、`copilot`
- 路由中心化配置：`route.exact_models + route.wildcard_models + route.hooks`
- 每个 public model 按协议声明自己的 upstream/provider 列表
- 精确模型 `upstreams` 映射与通配符模型 `providers` 选择
- OpenAI `chat/completions` 与 `responses` 双接口
- OpenAI `responses -> chat` 无状态兼容桥接
- Anthropic `messages -> chat` 受控兼容桥接
- route-scoped 工具调用 Hook：`exec` / `ai` / `http`
- Provider 健康探测、抑制、failover 与 Prometheus 指标
- 可选客户端 API Key 鉴权；按密钥统计请求与 token 用量
- Provider `/models` 发现失败不会阻塞启动，且会上游内部错误脱敏后再记录日志
- 文件 / HTTP 请求日志
- 内置管理面板：Dashboard、Providers、Routes、Tool Hooks、Logs、Config

## 不再支持

- 内置 MCP client 运行时
- SSH 远程执行与 SSH 配置块

如果你看到旧文档、旧配置或旧前端缓存里还提到 `mcp` / `ssh` 配置，那是历史残留，不是当前实现。

## 构建与运行

```bash
make build        # 前端构建 + Go 编译，输出 bin/warden
make test         # go vet + go test
make web          # 仅构建前端

./bin/warden
./bin/warden -c /path/to/warden.yaml
./bin/warden -i   # 安装 systemd 服务
./bin/warden -r   # 向运行实例发送 SIGHUP 热重载
```

`make build` 通过 `ldflags` 注入版本和构建日期。

配置文件搜索顺序：`warden.yaml` → `config/warden.yaml` → `/etc/warden.yaml`，同时支持 `.yml` 后缀。

## 配置示例

完整示例见 `config/warden.example.yaml`。

```yaml
addr: ":8080"
# admin_password: "your-secret-password"
# api_keys:
#   my-app: "wk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"

provider:
  openai:
    family: "openai"
    url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"
    timeout: "60s"

  anthropic:
    family: "anthropic"
    url: "https://api.anthropic.com/v1"
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: "60s"

route:
  /openai:
    protocol: chat
    exact_models:
      gpt-4o:
        upstreams:
          - provider: "openai"
            model: "gpt-4o"
    wildcard_models:
      "gpt-*":
        providers: ["openai"]

  /anthropic:
    protocol: anthropic
    exact_models:
      claude-sonnet-4:
        upstreams:
          - provider: "anthropic"
            model: "claude-sonnet-4"
```

### 配置要点

- `provider.*.url` / `webhook.*.url` 必须是绝对 `http/https` URL
- `provider.*.proxy` 只接受 `http`、`https`、`socks5`、`socks5h`
- `provider.*.family` 必填；`provider.*.protocol` 仍兼容但只作为旧字段别名，不能与 `family` 冲突
- `provider.*.enabled_protocols` / `provider.*.disabled_protocols` 用于在 provider family 候选协议面内做静态收缩，不改变 `route.protocol` 是运行时真相这一原则
- `qwen` / `copilot` 在未显式配置 `api_key` 时，会从本地 `config_dir` 读取 OAuth 凭证
- `api_keys` 为空时，网关不校验客户端 API Key；配置后支持 `Authorization: Bearer ...`、`Api-Key`、`X-Api-Key`
- `admin_password` / `api_keys` / `provider.*.api_key` 读取时兼容明文和 base64，写回配置文件时统一写为 base64；该兼容模式默认假设当前支持的 secret 格式不会与规范化 base64 明文冲突
- `route.protocol` 必须显式声明，且每个 route 只允许一个协议
- `route.exact_models.<name>` 直接声明 `upstreams`；`route.wildcard_models.<pattern>` 直接声明 `providers`
- `responses_stateful` exact model 只允许单 upstream；wildcard model 只允许单 provider
- `route.hooks` 只观察并审计模型返回的工具调用；Warden 不负责执行内置 MCP 工具

## 管理面板

设置 `admin_password` 后访问 `http://localhost:8080/_admin/`，用户名固定为 `admin`。

- `Dashboard`：provider 状态、路由概览、实时指标
- `Providers`：模型、健康状态、抑制控制，以及单个 provider 配置编辑
- `Routes`：先锁定 route 唯一协议，再编辑精确模型和通配符模型
- `Tool Hooks`：按 route 编辑 hook 规则与建议
- `Logs`：SSE 请求日志流
- `Config`：通用配置、客户端 API 密钥、webhook、日志目标编辑与应用

## 使用

```bash
curl http://localhost:8080/openai/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'

curl http://localhost:8080/openai/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","input":"Hello"}'

curl http://localhost:8080/openai/models
```

如果配置了 `api_keys`，客户端请求需要额外携带网关 API Key，例如：

```bash
curl http://localhost:8080/openai/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer wk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

客户端可通过 `X-Provider` 头强制指定 provider：

```bash
curl http://localhost:8080/openai/responses \
  -H "Content-Type: application/json" \
  -H "X-Provider: openai" \
  -d '{"model":"gpt-4o","input":"Hello"}'
```

对 OpenAI-compatible provider：

- `responses_to_chat: true`：仅无状态 `responses` → 上游 `/chat/completions`
- `anthropic_to_chat: true`：仅 `anthropic /messages` 的受控子集 → 上游 `/chat/completions`

`responses_to_chat` 只支持受控的 Chat 兼容子集：字符串/数组 `input`、顶层 `instructions`、`function` tools，以及少量共享 chat 参数。
桥接回写时会把 Chat `usage` 规范化为 Responses 风格的 `input_tokens` / `output_tokens`，并把 Chat `finish_reason` 映射为 Responses `status` / `incomplete_details`。
其中会显式兼容 `max_output_tokens -> max_completion_tokens`，并把 Responses 风格的 `tool_choice` 规范化为 Chat 风格对象；`function_call_output.output` 允许传入任意 JSON 并在转 Chat tool message 时规范化为字符串。
不支持的 Responses 专有字段或未知 input item 会直接返回 `400`，不会再伪装成 function tool 继续转发。
`responses_to_chat` 明确不支持 `previous_response_id`，因此不能承载 Responses 有状态续接。
Responses 流式桥会补齐更接近原生协议的事件序列和关联字段；如果上游 `400` 明确拒绝 `developer` role，会自动回退为 `system` 后重试一次。
原生 `/responses` 路径会透传 `previous_response_id` 等字段，但会话状态仍由上游 provider 维护；带 `previous_response_id` 的有状态请求会禁用协议转换和 failover。

`anthropic_to_chat` 只支持受控的 Messages 兼容子集：字符串或纯文本 blocks 的 `system` / `messages`、`tool_use` / `tool_result`、`function` tools，以及少量共享采样参数。
不支持的 Anthropic 专有字段、非文本 content block、`tool_result` 与普通用户文本混合消息会直接返回 `400`。

更完整的 Responses 现状整理见 [docs/responses-stateful-stateless-support.md](/home/wweir/Mine/warden/docs/responses-stateful-stateless-support.md)。

其它仍有独立价值的专题说明见 [docs/README.md](/home/wweir/Mine/warden/docs/README.md)。

对 route：

- route 暴露哪些入口，只由 `route.protocol` 决定
- `chat` 只暴露 `/chat/completions`
- `responses_stateless` 只暴露无状态 `/responses`，明确拒绝 `previous_response_id`
- `responses_stateful` 暴露 `/responses`，同时接受有状态和无状态请求
- `anthropic` 只暴露 `/messages`
- provider family 只承担上游适配职责：`openai => chat + responses_*`，开启 `anthropic_to_chat` 时额外支持 `anthropic`；`anthropic => chat + anthropic`；`qwen/copilot/ollama => chat`
- 如果同一个 provider 只想参与部分协议 route，优先使用 `enabled_protocols` / `disabled_protocols` 收窄能力面，而不是复制一份 provider 配置

## 项目结构

```text
cmd/warden/          # 入口
config/              # 配置定义、校验、示例
internal/
  gateway/           # HTTP 网关、管理 API、指标、协议适配
  install/           # systemd 安装逻辑
  reqlog/            # 请求日志与 SSE 广播
  selector/          # provider 选择与状态
pkg/
  protocol/          # 协议公共类型与协议实现
  provider/          # OAuth token 管理（Qwen、Copilot）
  toolhook/          # 通用工具调用 Hook 执行
web/admin/           # Vue 3 管理前端（构建产物嵌入二进制）
```

## 文档分工

- `README.md`：面向使用者的项目入口、能力概览、构建运行与最小配置示例
- `ARCHITECTURE.md`：系统边界、分层职责、关键数据流与设计决策
- `docs/README.md`：当前专题文档索引，只保留未被根文档吸收的主题
- `config/README.md`：配置模型、校验规则与配置层边界

## License

MPL-2.0
