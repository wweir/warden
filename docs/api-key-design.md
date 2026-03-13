# API Key 管理与敏感信息编码方案

## 概述

本方案实现两个需求：
1. 支持在 admin 页面生成、管理 API Key，并保存到配置文件
2. 配置文件中的敏感信息（api_key、admin_password、api_keys）写入时 base64 编码，读取时兼容 base64 和明文

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

### 3. API 端点 (`internal/gateway/api_admin.go`)

| 方法 | 路径 | 功能 |
|------|------|------|
| GET | /_admin/api/apikeys | 列出所有密钥名称 |
| POST | /_admin/api/apikeys | 创建新密钥，返回明文密钥（仅此一次） |
| DELETE | /_admin/api/apikeys | 删除密钥 |

### 4. 前端页面 (`web/admin/src/views/ApiKeys.vue`)

- 密钥列表展示
- 创建密钥弹窗，显示生成的密钥（提示复制）
- 删除密钥确认

---

## 待完善内容

### 1. 密钥持久化到配置文件

**问题**：当前实现中，API Key 仅保存在内存中，服务重启后丢失。

**方案**：创建/删除密钥时同步更新配置文件。

```go
// handleAPIKeysCreate 中添加：
func (g *Gateway) handleAPIKeysCreate(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
    // ... 生成密钥后 ...

    // 持久化到配置文件
    if g.configPath != "" {
        if err := g.saveConfigFile(); err != nil {
            // 回滚内存中的更改
            delete(g.cfg.APIKeys, body.Name)
            http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
            return
        }
    }
}

// 新增 saveConfigFile 方法
func (g *Gateway) saveConfigFile() error {
    // marshal config to YAML, inject secrets, write to g.configPath
}
```

**修改位置**：`internal/gateway/api_admin.go`

### 2. 密钥认证中间件

**问题**：当前仅支持 admin 密码认证，API Key 未实际用于认证。

**方案**：新增认证中间件支持 API Key。

```go
// internal/gateway/auth.go

// AuthMethod 认证方式
type AuthMethod int
const (
    AuthNone AuthMethod = iota
    AuthAdminPassword
    AuthAPIKey
)

// Authenticate 检查请求认证
func (g *Gateway) Authenticate(r *http.Request) (AuthMethod, string, bool) {
    // 1. 检查 Bearer token (API Key)
    auth := r.Header.Get("Authorization")
    if strings.HasPrefix(auth, "Bearer ") {
        token := strings.TrimPrefix(auth, "Bearer ")
        for name, key := range g.cfg.APIKeys {
            if subtle.ConstantTimeCompare([]byte(token), []byte(key.Value())) == 1 {
                return AuthAPIKey, name, true
            }
        }
    }

    // 2. 检查 Basic Auth (Admin)
    user, pass, ok := r.BasicAuth()
    if ok && user == "admin" {
        if subtle.ConstantTimeCompare([]byte(pass), []byte(g.cfg.AdminPassword.Value())) == 1 {
            return AuthAdminPassword, "admin", true
        }
    }

    return AuthNone, "", false
}
```

### 3. API Key 权限控制

**问题**：API Key 应该有更细粒度的权限控制。

**方案**：扩展 API Key 结构支持权限字段。

```go
type APIKeyConfig struct {
    Key         SecretString   `json:"key"`
    Permissions []string       `json:"permissions"` // "read", "write", "admin"
    CreatedAt   time.Time      `json:"created_at"`
    LastUsedAt  time.Time      `json:"last_used_at,omitempty"`
    Description string         `json:"description,omitempty"`
}

type ConfigStruct struct {
    // ...
    APIKeys map[string]*APIKeyConfig `json:"api_keys"`
}
```

**权限级别**：
- `read`：只读访问（GET 端点）
- `write`：读写访问（GET/POST/PUT/DELETE 配置相关）
- `admin`：完全访问（包括重启、密钥管理）

### 4. 配置文件保存时的敏感信息处理

**问题**：当前配置保存逻辑在 `handleAdminConfigPut` 中，需要确保敏感字段正确处理。

**方案**：统一敏感字段处理逻辑。

```go
// config/secrets.go

// SensitiveFields 定义所有敏感字段路径
var SensitiveFields = []string{
    "admin_password",
    "api_keys.*",
    "provider.*.api_key",
}

// MaskSensitiveFields 用于 API 返回时脱敏
func MaskSensitiveFields(cfgMap map[string]any) {
    // 递归处理，将敏感字段替换为 "***"
}

// EncodeSensitiveFields 用于保存时 base64 编码
func EncodeSensitiveFields(cfgMap map[string]any) {
    // 递归处理，对敏感字段进行 base64 编码
}
```

### 5. 前端密钥管理增强

**问题**：当前前端功能较简单。

**方案**：

1. **显示密钥元数据**：
   - 创建时间
   - 最后使用时间
   - 权限级别

2. **密钥使用统计**：
   - 各密钥的请求次数
   - 最近使用的端点

3. **批量操作**：
   - 批量删除
   - 导出/导入密钥列表

### 6. 安全增强

**方案**：

1. **密钥格式验证**：
   ```go
   func IsValidAPIKey(key string) bool {
       // 格式：wk_ + 32字符 base62
       if !strings.HasPrefix(key, "wk_") {
           return false
       }
       if len(key) != 35 {
           return false
       }
       // 检查字符集
       return true
   }
   ```

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