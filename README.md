# Warden

Warden 是一个面向多上游 LLM 的轻量级 AI Gateway。它暴露统一的 OpenAI 兼容入口，把请求按路由和模型映射到不同 provider，并在请求链路上提供协议转换、System Prompt 注入、工具调用 Hook、日志与管理面板。

## 当前能力

- 统一接入 `openai`、`anthropic`、`ollama`、`qwen`、`copilot`
- 路由中心化配置：`route.protocol + route.exact_models + route.wildcard_models + route.hooks`
- 精确模型 `upstreams` 映射与通配符模型 `providers` 选择
- OpenAI `chat/completions` 与 `responses` 双接口
- OpenAI `chat ↔ responses` 协议桥接
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
    url: "https://api.openai.com/v1"
    protocol: "openai"
    api_key: "${OPENAI_API_KEY}"
    timeout: "60s"

  anthropic:
    url: "https://api.anthropic.com/v1"
    protocol: "anthropic"
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: "60s"

route:
  /openai:
    protocol: "chat"
    exact_models:
      gpt-4o:
        upstreams:
          - provider: "openai"
            model: "gpt-4o"
    wildcard_models:
      "gpt-*":
        providers: ["openai"]

  /anthropic:
    protocol: "anthropic"
    exact_models:
      claude-sonnet-4:
        upstreams:
          - provider: "anthropic"
            model: "claude-sonnet-4"
```

### 配置要点

- `provider.*.url` / `webhook.*.url` 必须是绝对 `http/https` URL
- `provider.*.proxy` 只接受 `http`、`https`、`socks5`、`socks5h`
- `qwen` / `copilot` 在未显式配置 `api_key` 时，会从本地 `config_dir` 读取 OAuth 凭证
- `api_keys` 为空时，网关不校验客户端 API Key；配置后支持 `Authorization: Bearer ...`、`Api-Key`、`X-Api-Key`
- `route.exact_models` 只接受 `upstreams`
- `route.wildcard_models` 只接受 `providers`
- `route.hooks` 只观察并审计模型返回的工具调用；Warden 不负责执行内置 MCP 工具

## 管理面板

设置 `admin_password` 后访问 `http://localhost:8080/_admin/`，用户名固定为 `admin`。

- `Dashboard`：provider 状态、路由概览、实时指标
- `Providers`：模型、健康状态、抑制控制，以及单个 provider 配置编辑
- `Routes`：按 route 编辑协议、精确模型、通配符模型并做请求测试
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

- `chat_to_responses: true`：本地 `chat/completions` → 上游 `/responses`
- `responses_to_chat: true`：本地 `responses` → 上游 `/chat/completions`

`responses_to_chat` 只支持 Chat 兼容子集：字符串/数组 `input`、`function` tools。

对 route：

- `route.protocol` 表示主协议面
- `anthropic` route 只暴露 `/messages`
- OpenAI-compatible route 会按 route 内 provider 能力自动补注册 `/chat/completions` 或 `/responses`，避免 provider 级协议转换在入口层被 404 截断

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

## License

MPL-2.0
