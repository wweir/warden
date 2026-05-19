# config

`config` 包负责三件事：

- 定义网关配置结构与少量运行时派生字段
- 在 `Validate()` 中做静态校验与规范化
- 编译 route 模型匹配结构，供运行时快速查找

相关文档：

- 系统级说明：[ARCHITECTURE.md](../ARCHITECTURE.md)
- 项目入口：[README.md](../README.md)
- Provider 专题：[docs/provider.md](../docs/provider.md)
- 专题索引：[docs/README.md](../docs/README.md)

## Scope

`config` 只负责配置真相：

- 配置结构定义
- 本地可判定的静态校验
- route 运行时匹配结构编译

`config` 不负责：

- 启动期网络探测
- provider 健康检查
- 管理端展示层 probe
- 请求期 provider 选择

## File Roles

- `config.go`：核心配置类型定义与运行时访问方法
- `provider_protocol.go`：provider format / protocol 常量与规范化辅助
- `validate.go`：配置校验与规范化逻辑
- `route_runtime.go`：route 模型编译、通配符匹配、协议能力判断、provider 级能力推导与 endpoint 定义规则
- `secret.go`：`SecretString` 的安全序列化与显示
- `warden.example.toml`：默认 TOML 示例配置

## Provider 配置模型

Provider 配置采用"**能力声明优先，接入方式推导**"模型：

```toml
# 国内主流 provider：最简配置
[provider.kimi]
url = "https://api.moonshot.cn/v1"
api_key = "sk-xxx"
models = ["kimi-k2", "kimi-k2.5"]
# 默认 format="openai"，默认 protocols=["chat","responses","embeddings"]
```

```toml
# 收窄能力
[provider.ollama]
url = "http://localhost:11434/v1"
protocols = ["chat"]
```

```toml
# Anthropic-native
[provider.anthropic]
url = "https://api.anthropic.com/v1"
format = "anthropic"
api_key = "sk-ant-xxx"
```

```toml
# 桥接
[provider.deepseek]
url = "https://api.deepseek.com/v1"
anthropic_to_chat = true
```

```toml
# 多 endpoint（共享同一 API Key）
[provider.unified]
api_key = "${API_KEY}"

[provider.unified.endpoint.openai]
url = "https://gateway.corp.com/openai/v1"
format = "openai"

[provider.unified.endpoint.anthropic]
url = "https://gateway.corp.com/anthropic/v1"
format = "anthropic"
```

### 核心字段

- `format` — 上游原生协议格式：`openai`（默认）、`anthropic`、`copilot`
- `protocols` — 该 provider 支持的服务协议；省略时按 `format` 推导默认值
- `anthropic_to_chat` / `responses_to_chat` / `anthropic_to_responses` — provider 级桥接开关
- `endpoint.<name>` — 显式多 endpoint 定义；与 provider 级 shorthand 字段互斥

### 能力推导规则

`format` 省略时默认 `openai`：

| `format` | 默认 `protocols` |
|---|---|
| `openai` | `chat`, `responses`, `embeddings` |
| `anthropic` | `chat`, `anthropic` |
| `copilot` | `chat` |

桥接开关生效后追加对应协议到支持集合。

### Endpoint 定义

当 provider 需要同时暴露多个协议入口（如 Coding Plan 同时提供 OpenAI 和 Anthropic endpoint）时，使用显式 `endpoint`：

- 声明 `endpoint.*` 后，provider 级 `url`、`format`、`protocols`、桥接开关必须为空
- 每个 `endpoint.<name>` 是一个完整的接入点定义：
  - `url` — 该 endpoint 的上游 base URL（必填）
  - `format` — 该 endpoint 的协议格式（默认 `openai`）
  - `protocols` — 该 endpoint 支持的服务协议（省略时按 `format` 推导）
  - `headers` — 该 endpoint 的额外 HTTP headers（合并 provider 级 `headers`）
  - `models` — 该 endpoint 的模型白名单（覆盖 provider 级 `models`）
  - `anthropic_to_chat` / `responses_to_chat` / `anthropic_to_responses` — endpoint 级桥接开关

### 旧配置兼容

旧配置需要手动迁移：

- `family` → `format`
- `protocol` → `format`
- `service_protocols` → `protocols`
- `access.<mode>` → `endpoint.<name>`（注意：`endpoint` 要求 `url` 在每个 endpoint 内显式声明，不再继承 provider 级 `url`）

```toml
# 迁移前
[provider.kimi]
family = "anthropic"
url = "https://api.kimi.com/coding/"
api_key = "sk-provider-token"
service_protocols = ["chat", "anthropic", "responses"]
anthropic_to_responses = true

# 迁移后
[provider.kimi]
url = "https://api.kimi.com/coding/"
api_key = "sk-provider-token"
format = "anthropic"
protocols = ["chat", "anthropic", "responses"]
anthropic_to_responses = true
```

## Validation Rules

- 只做本地可判定的静态校验，不做启动期网络探测
- `provider.*.url` / `webhook.*.url` 必须是绝对 `http/https` URL
- `webhook.*.body_template` 在校验阶段就会按 Go template + sprig 解析；坏模板不能留到运行时才暴露
- `webhook.*.timeout` 如果设置，必须是大于 0 的时长
- `provider.*.proxy` 只接受 `http`、`https`、`socks5`、`socks5h`
- `provider.*.backend` 是可选上游实现标记；当前只接受 `cliproxy`，且要求 `backend_provider`
- `cliproxy.enabled` 启用嵌入式 CLIProxyAPI/cliproxy 服务；启用后至少需要一个 `backend: cliproxy` provider，且 provider URL 必须是共享的 `http://loopback:port/v1`
- `cliproxy.auth_dir` 留空时默认使用 `/etc/warden`
- `backend: cliproxy` 不支持 `api_key_command`
- `~` 路径在校验阶段统一展开
- `provider.*.timeout` 只限制从发出上游请求到收到首个响应 body/token 的时间
- `provider.*.api_key_command` 允许通过 shell 命令提供 API Key；默认超时 `5s`，默认缓存 TTL `5m`
- `provider.*.api_key_command` 与 `provider.*.api_key` 互斥
- `route.<prefix>.api_keys` 是该路由自己的客户端访问密钥集合
- 同一路由下的 `api_keys` 明文值必须唯一
- 顶层 `api_keys` 已废弃；校验阶段会直接报错
- `admin_password` / `route.<prefix>.api_keys` / `provider.*.api_key` 读取时接受明文或 base64，写回时统一编码为 base64
- Provider 校验：
  - `format` 必须为 `openai`、`anthropic`、`copilot` 之一（或省略，默认 `openai`）
  - `protocols` 只能包含 `chat`、`responses`、`anthropic`、`embeddings`
  - 桥接开关必须与 `format` 兼容（如 `anthropic_to_chat` 要求 `format = "openai"`）
  - `backend = "cliproxy"` 要求 `format = "openai"`
  - `endpoint.<name>.url` 必须是合法 http/https URL

## Compatibility Notes

推荐心智模型：

- `provider` 描述上游能力与认证方式
- `route` 描述对外暴露的协议面和模型面
- 运行时路由真相由 `route.protocol + route model` 决定
- route 配置的主结构是 `exact_models` / `wildcard_models`
- `route.protocol` 是必填字段，且每个 route 只允许一个 `chat` / `responses` / `anthropic`
- `/embeddings` 是额外 service protocol，不是新的 `route.protocol`
- `route.service_protocols` 可选；留空按 `route.protocol` 推导
- `route.wildcard_models.<pattern>` 的 `*` 匹配完整模型 ID，包括 `vendor/model` 和 `vendor/model:free`
- provider 协议能力由 `format` + `protocols` + 桥接开关决定，不再依赖单值 `family`
- 运行时选择单元是 `provider + format + model + service_protocol`
- failover 以 `provider + format` 为最小单元，避免 Anthropic endpoint 失败把同一 provider 的 OpenAI endpoint 也打死

## Related Files

- [config.go](./config.go)
- [validate.go](./validate.go)
- [route_runtime.go](./route_runtime.go)
- [secret.go](./secret.go)
- [warden.example.toml](./warden.example.toml)
