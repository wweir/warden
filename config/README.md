# config

`config` 包负责三件事：

- 定义网关配置结构与少量运行时派生字段
- 在 `Validate()` 中做静态校验与规范化
- 编译 route 模型匹配结构，供运行时快速查找

## File Roles

- `config.go`：核心配置类型定义与运行时访问方法
- `validate.go`：配置校验与规范化逻辑
- `route_runtime.go`：route 模型编译、通配符匹配、协议能力判断
- `secret.go`：`SecretString` 的安全序列化与显示

## Validation Rules

- 只做本地可判定的静态校验，不做启动期网络探测
- `provider.*.url` / `webhook.*.url` 必须是绝对 `http/https` URL
- `provider.*.proxy` 只接受 `http`、`https`、`socks5`、`socks5h`
- `~` 路径在校验阶段统一展开
- `qwen` / `copilot` 在未设置 `api_key` 时校验本地 `config_dir` 下的凭证可读性
- `api_keys` 是客户端访问网关的密钥集合；为空时不做客户端鉴权

## Compatibility Notes

- route 配置的主结构是 `exact_models` / `wildcard_models`
- route model 的额外提示词由 `prompt_enabled` + `system_prompt` 共同表达；缺少 `prompt_enabled` 的旧配置仍兼容为“只要 `system_prompt` 非空就启用”
- `route.protocol` 必填，不再接受空协议的隐式行为
- failover 只在命中的 route model 候选列表内发生，因此可以只给某一个配置模型单独做 HA
- 带 `previous_response_id` 的 Responses 有状态请求会禁用 failover，并绕过 `responses_to_chat`
- `route` 的运行时派生字段只在 `Validate()` 后可依赖
- `mcp` 与 `ssh` 配置块已移除，不再参与配置模型
