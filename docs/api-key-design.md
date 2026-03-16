# API Key 管理与敏感信息编码方案

## 概述

当前实现覆盖三类需求：
1. `api_keys` 作为通用配置的一部分，在 `Config` 页面内生成、删除并随整份配置保存
2. 网关在 `api_keys` 非空时校验客户端 API Key；为空时不做客户端鉴权
3. 配置文件中的敏感信息（`api_key`、`admin_password`、`api_keys`）写入时 base64 编码，读取时兼容 base64 和明文

## 已实现内容

### 1. SecretString 类型 (`config/secret.go`)

```go
type SecretString string

// MarshalText 写入时 base64 编码
func (s SecretString) MarshalText() ([]byte, error)

// UnmarshalText 读取时兼容 base64 和明文
func (s *SecretString) UnmarshalText(data []byte) error

// Value() 返回原始值
// String() 返回 "***" 用于日志安全
```

### 2. 配置结构更新 (`config/config.go`)

```go
type ConfigStruct struct {
    AdminPassword SecretString            `json:"admin_password"`
    APIKeys       map[string]SecretString `json:"api_keys"`
    // ...
}

type ProviderConfig struct {
    APIKey SecretString `json:"api_key"`
    // ...
}
```

### 3. 管理 API (`internal/gateway/api_admin.go`)

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | /_admin/api/apikeys | 返回当前密钥名称与按密钥聚合的用量统计 |
| GET | /_admin/api/config | 返回脱敏后的整份配置，其中 `api_keys` value 为 `__REDACTED__` |
| PUT | /_admin/api/config | 保存整份配置；`api_keys` 由配置页统一提交 |

### 4. 前端页面 (`web/admin/src/views/Config.vue`)

- API Key 区块直接放在通用配置页
- 支持本地生成新密钥、删除密钥、展示按密钥聚合的请求与 token 用量
- 新生成的明文密钥只在前端生成当次展示；保存后重新加载时只返回脱敏值

---

## 当前实现边界

### 1. 客户端 API Key 与 Provider API Key 分离

- 客户端 API Key 只用于进入网关时认证
- 认证成功后会立即移除 `Authorization` / `Api-Key` / `X-Api-Key`
- 上游 Provider 的认证仍由 `selector.SetAuthHeaders` 基于 `provider.*.api_key` 或 OAuth 凭证注入

这条边界是必须的；否则客户端密钥会污染上游 Provider 鉴权链路。

### 2. 用量统计粒度

当前按密钥维护两类指标：

- 请求数：总数、成功数、失败数
- token：输入 token、输出 token

统计标签仍保留 route / route_model / endpoint 维度，管理端按密钥聚合展示。

### 3. 暂未实现

以下能力不是当前实现目标：

- 权限分级
- 创建时间 / 最后使用时间
- 密钥导入导出
- 单独的 API Key 管理页面

2. **密钥轮换**：
   - 支持重新生成密钥
   - 旧密钥设置过期时间

3. **使用审计**：
   - 记录每次密钥使用
   - 异常使用告警

---

## 完整实现优先级

| 优先级 | 功能 | 工作量 |
|--------|------|--------|
| P0 | 密钥持久化到配置文件 | 2h |
| P0 | 配置保存时正确编码敏感字段 | 1h |
| P1 | API Key 认证中间件 | 2h |
| P1 | 权限控制 | 3h |
| P2 | 密钥元数据（创建时间、描述） | 2h |
| P2 | 前端增强 | 2h |
| P3 | 密钥使用统计 | 4h |
| P3 | 安全审计 | 4h |

---

## 测试用例

### SecretString 单元测试

```go
func TestSecretString_Base64Roundtrip(t *testing.T) {
    original := SecretString("my-secret-123")

    // 编码
    encoded, _ := original.MarshalText()

    // 解码
    var decoded SecretString
    decoded.UnmarshalText(encoded)

    if decoded.Value() != original.Value() {
        t.Errorf("roundtrip failed")
    }
}

func TestSecretString_PlaintextInput(t *testing.T) {
    var s SecretString
    s.UnmarshalText([]byte("plaintext"))

    if s.Value() != "plaintext" {
        t.Errorf("should accept plaintext")
    }
}
```

### API 端点测试

```go
func TestAPIKeysCRUD(t *testing.T) {
    // 创建
    resp := POST("/_admin/api/apikeys", `{"name":"test"}`)
    key := resp.Key

    // 验证格式
    if !strings.HasPrefix(key, "wk_") {
        t.Errorf("invalid key format")
    }

    // 列表
    list := GET("/_admin/api/apikeys")
    if !contains(list.Keys, "test") {
        t.Errorf("key not in list")
    }

    // 删除
    DELETE("/_admin/api/apikeys", `{"name":"test"}`)

    // 验证删除
    list = GET("/_admin/api/apikeys")
    if contains(list.Keys, "test") {
        t.Errorf("key should be deleted")
    }
}
```
