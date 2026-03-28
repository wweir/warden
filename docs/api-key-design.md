# API Key 管理与敏感信息编码方案

> 更新日期：2026-03-28
>
> 状态：current

本文只描述当前已经落地的实现，不再混入旧计划。

## 1. 目标

当前方案覆盖三件事：

1. `api_keys` 作为整份配置的一部分持久化，并由管理端统一编辑
2. 网关在 `api_keys` 非空时校验客户端 API Key；为空时不做客户端鉴权
3. `admin_password`、`api_keys`、`provider.*.api_key` 写入配置时统一 base64 编码，读取时兼容 base64 和明文

## 2. 当前实现

### 2.1 SecretString

`config/secret.go` 定义 `SecretString`：

- `MarshalText()`：写回配置时按 base64 编码
- `UnmarshalText()`：读取时兼容 base64 和明文
- `Value()`：返回原始值
- `String()`：返回脱敏值，避免日志直接泄露

这让配置落盘和运行时读取复用同一套 secret 语义。

### 2.2 配置模型

敏感字段都已经切到 `SecretString`：

- `config.ConfigStruct.AdminPassword`
- `config.ConfigStruct.APIKeys`
- `config.ProviderConfig.APIKey`

因此：

- 日志打印不会直接带出明文
- 管理端保存配置时可以统一编码
- provider 认证链路和客户端认证链路各自读取自己的 secret

### 2.3 管理端接口

当前管理端接口：

| 方法 | 路径 | 功能 |
|------|------|------|
| `GET` | `/_admin/api/apikeys` | 返回当前 API Key 名称与按密钥聚合的用量统计 |
| `POST` | `/_admin/api/apikeys` | 生成并写入新的客户端 API Key |
| `DELETE` | `/_admin/api/apikeys` | 删除指定客户端 API Key |
| `GET` | `/_admin/api/config` | 返回脱敏后的整份配置 |
| `PUT` | `/_admin/api/config` | 保存整份配置，`api_keys` 作为配置的一部分一起提交 |

说明：

- `/_admin/api/config` 返回的是脱敏视图，不回显明文 secret
- 新创建的明文密钥只会在创建当次返回，后续读取不会再回显

### 2.4 前端入口

`web/admin` 当前没有独立 API Key 页面，入口在 `Config` 页面。

当前支持：

- 本地生成新密钥
- 删除现有密钥
- 查看按密钥聚合的请求数与 token 用量

## 3. 边界

### 3.1 客户端 API Key 与 Provider API Key 严格分离

- 客户端 API Key 只用于进入网关时认证
- 认证成功后，网关会移除客户端传入的 `Authorization`、`Api-Key`、`X-Api-Key`
- 上游 provider 认证由 `provider.*.api_key` 或本地 OAuth 凭证单独注入

这是硬边界。否则客户端密钥会污染上游 provider 鉴权链路。

### 3.2 用量统计是聚合视图，不是逐次审计日志

当前按密钥聚合展示：

- 请求数：总数、成功数、失败数
- token：输入 token、输出 token

管理端会按 key 聚合，但底层指标仍保留 route / route_model / endpoint 等运行时维度。

### 3.3 兼容明文读取有前提

secret 读取兼容明文和 base64，但这个兼容策略依赖一个前提：

- 当前支持的 secret 格式不会和“规范化后的可逆 base64 文本”歧义冲突

这适用于当前实现，不等于任意自定义 secret 文本都能零歧义兼容。

## 4. 明确不包含的能力

以下不是当前实现目标：

- 权限分级
- key 描述、创建时间、最后使用时间
- 密钥导入导出
- 单独的 API Key 管理页面
- 密钥轮换工作流
- 逐次调用审计与异常告警

如果后续要做这些能力，应新增专题设计文档，不要继续把规划和现状混写在这里。

## 5. 相关代码与文档

- [config/secret.go](/home/wweir/Mine/warden/config/secret.go)
- [config/config.go](/home/wweir/Mine/warden/config/config.go)
- [internal/gateway/admin/router.go](/home/wweir/Mine/warden/internal/gateway/admin/router.go)
- [README.md](/home/wweir/Mine/warden/README.md)
- [ARCHITECTURE.md](/home/wweir/Mine/warden/ARCHITECTURE.md)
