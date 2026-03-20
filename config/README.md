# config

`config` 包负责三件事：

- 定义网关配置结构与少量运行时派生字段
- 在 `Validate()` 中做静态校验与规范化
- 编译 route 模型匹配结构，供运行时快速查找

## File Roles

- `config.go`：核心配置类型定义与运行时访问方法
- `provider_protocol.go`：provider family 常量与协议规范化辅助
- `validate.go`：配置校验与规范化逻辑
- `route_runtime.go`：route 模型编译、通配符匹配、协议能力判断
- `secret.go`：`SecretString` 的安全序列化与显示

## Validation Rules

- 只做本地可判定的静态校验，不做启动期网络探测
- `provider.*.url` / `webhook.*.url` 必须是绝对 `http/https` URL
- `provider.*.proxy` 只接受 `http`、`https`、`socks5`、`socks5h`
- `provider.*.family` 必填；`provider.*.protocol` 只保留为兼容别名，不能与 `family` 冲突
- `provider.*.enabled_protocols` / `provider.*.disabled_protocols` 只能在 provider family 的候选协议面内做收缩
- `~` 路径在校验阶段统一展开
- `qwen` / `copilot` 在未设置 `api_key` 时校验本地 `config_dir` 下的凭证可读性
- `api_keys` 是客户端访问网关的密钥集合；为空时不做客户端鉴权
- `admin_password` / `api_keys` / `provider.*.api_key` 读取时接受明文或 base64，写回配置文件时统一编码为 base64
- 该兼容模式建立在当前支持的 API key / password 格式不会与“可逆且规范化的 base64 文本”冲突这一前提上；任意自定义 secret 明文不保证避免歧义

## Compatibility Notes

- route 配置的主结构是 `exact_models` / `wildcard_models`
- `route.protocol` 是必填字段，且每个 route 只允许一个 `chat` / `responses_stateless` / `responses_stateful` / `anthropic`
- route model 的额外提示词由模型自身的 `prompt_enabled` + `system_prompt` 表达
- `route.exact_models.<name>` 直接声明 `upstreams`
- `route.wildcard_models.<pattern>` 直接声明 `providers`
- provider family 候选兼容能力由 `route_runtime.go` 统一推导，当前为 `openai => chat + responses_*`，启用 `anthropic_to_chat` 时额外支持 `anthropic`；`anthropic => chat + anthropic`；`qwen/copilot/ollama => chat`
- provider 可以通过 `enabled_protocols` / `disabled_protocols` 进一步收窄自己的有效协议面
- failover 只在命中的 route model 候选列表内发生，因此可以只给某一个配置模型单独做 HA
- `responses_stateless` 明确拒绝 `previous_response_id`
- `responses_stateful` 接受 `previous_response_id`，但会禁用 failover，并绕过 `responses_to_chat`
- `responses_stateful` exact model 只允许单 upstream；wildcard model 只允许单 provider
- 启用 `responses_to_chat` 的 provider 不能被 `responses_stateful` route 引用，因为该桥接模式不支持 `previous_response_id`
- `anthropic_to_chat` 只允许配置在 `openai` provider 上，并且只对 `route.protocol=anthropic` 的 `/messages` 入口生效
- `route` 的运行时派生字段只在 `Validate()` 后可依赖
- `mcp` 与 `ssh` 配置块已移除，不再参与配置模型
