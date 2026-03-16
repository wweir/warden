# config

`config` 包负责三类事情：

- 定义网关配置结构体和运行时派生字段
- 校验并规范化配置输入
- 提供路由模型匹配和敏感字段封装

当前文件分工：

- `config.go`：核心配置类型定义、运行时访问方法
- `validate.go`：配置校验与规范化逻辑
- `route_runtime.go`：路由模型编译、通配符匹配、协议能力判断
- `secret.go`：`SecretString` 的安全序列化与显示

校验原则：

- 只做本地可判定的静态校验，不做启动期网络探测
- provider/webhook URL 必须是绝对 `http/https` URL
- proxy URL 只接受 `http`、`https`、`socks5`、`socks5h`
- `~` 路径在校验阶段统一展开，避免运行期重复分支

兼容性约束：

- 保留 legacy route 配置到 `models` 的转换逻辑
- `route` 的运行时派生字段在 `Validate()` 后才可依赖
