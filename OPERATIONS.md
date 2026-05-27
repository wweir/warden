# Warden Operations Guide

本文件描述 Warden 的部署配置、运维规范和跨平台差异。

## Deployment Boundary

Warden 的运行时核心是跨平台的 HTTP 网关进程，但进程托管不是跨平台同构问题。

### Platform-Specific Behavior

| 特性 | Linux | macOS | Windows |
|------|-------|-------|---------|
| 托管方式 | systemd | launchd | Task Scheduler |
| 前台运行 | ✓ | ✓ | ✓ |
| 配置路径 | `/etc/warden/warden.toml` | `%APPDATA%\warden\config.toml` | `%APPDATA%\warden\config.toml` |
| 热重载 | `kill -HUP` | `kill -HUP` | 不支持 |
| GUI 安装器 | ✗ | ✗ | ✓ (setup.exe) |

### Binary Distribution

- **Linux/macOS**: 单一二进制 `warden`，可独立运行或托管
- **Windows**: 两个二进制
  - `warden.exe` - 运行时服务
  - `warden-setup.exe` - 自解压安装器（GUI）

### Installation Modes

#### Interactive Install (Linux/macOS)

```bash
sudo warden -i
```

流程：
1. 询问是否对外提供服务
2. 若需要，提示输入并确认 admin 密码
3. 生成 bootstrap 配置到默认路径
4. 启动/重启托管服务

#### Non-Interactive Install

```bash
# Linux with systemd
make install

# Or directly
warden -i -y  # Skip prompts, use defaults
```

#### Windows GUI Install

运行 `warden-setup.exe`：
1. 选择安装路径
2. 配置服务参数
3. 设置 admin 密码
4. 安装并启动服务

## Configuration

### Main Configuration Files

支持 TOML 和 YAML 两种格式，启动时按以下顺序搜索：

1. `./warden.toml` (当前目录)
2. `./config/warden.toml`
3. 平台默认路径：
   - Linux: `/etc/warden/warden.toml`
   - macOS/Windows: `%APPDATA%\warden\config.toml`
4. 兼容搜索同位置的 `.yaml`/`.yml`

使用 `-c` 显式指定：
```bash
warden -c /path/to/config.toml
warden -c /path/to/config.yaml
```

### Bootstrap Configuration

托管安装只在配置不存在时生成 bootstrap 配置，包含：
- 基础服务配置（监听地址、日志级别）
- Admin 密码（如用户选择对外服务）
- **不包含** provider / route，避免首次启动隐式依赖外部服务

### Hot Reload

- **Linux/macOS**: 发送 `SIGHUP` 信号触发配置重载
- **Windows**: 不支持信号重载，需重启服务

```bash
# Linux
kill -HUP $(pidof warden)

# macOS
kill -HUP $(pgrep warden)
```

### Admin Panel

- 默认监听 `127.0.0.1:9832`
- 管理后台：`http://localhost:9832/_admin/`
- 若选择对外服务，监听 `:9832`

## Log Management

### Log Output

- **File backend**: 写入配置指定的日志文件路径
- **HTTP backend**: 推送到配置的 HTTP endpoint

### Log Content

请求日志记录：
- 请求体
- 解压后的响应体（透明代理的 gzip/br/zstd 响应会先解压）
- TTFT (Time to First Token) for streaming requests
- Token usage 观测结果

### Session Deduplication

- 同一会话的多轮请求（`Route + Fingerprint[:6]` sysHash 相同）在内存广播器和文件日志中只保留最新一条完整记录
- 内存广播器按 route 只保留最近 20 个 session

## Monitoring

### Prometheus Metrics

暴露 `/metrics` 端点，包含：
- 请求计数、延迟、错误率
- Token 使用量
- Provider 级别指标（含 `provider_format` label）

### Dashboard Metrics

内存中的滚动时间序列，支持：
- 即时聚合指标
- 按 API Key 的用量统计

### Health Checks

```bash
# System status
curl http://localhost:9832/_admin/api/status

# Provider health
curl -X POST http://localhost:9832/_admin/api/providers/health \
  -H "Content-Type: application/json" \
  -d '{"provider_name": "openai"}'
```

## Security

### API Key Storage

- `admin_password`, `route.<prefix>.api_keys`, `provider.*.api_key` 读取时兼容明文和 base64
- 写回配置时统一存为 base64

### API Key Command

`provider.*.api_key_command` 是 RCE 级配置：
- 只能由受信任 operator 配置
- 命令以 Warden 服务用户身份执行
- stdout/stderr 不写入普通错误输出
- 默认超时 `5s`，缓存 TTL `5m`

### Network Security

- 所有网络操作必须设置超时
- 优先使用 `context` 控制生命周期
- 遵循最小权限原则

## Troubleshooting

### Logs Page

管理端的 Logs 页面提供排障结构：
- 左侧：按 route 分组，再按 session fingerprint 前缀组织成树状浏览
- 右侧：日志表 + 详情弹层

### Common Issues

1. **Provider 连接失败**：检查 selector 抑制状态，手动取消抑制
2. **配置校验失败**：使用 `POST /_admin/api/config/validate` 预校验
3. **Token 使用统计缺失**：确认使用 exact usage 观测

## Build Notes

```bash
# Build for current platform
make build

# Cross-platform release
make package

# Run tests
make test

# Docker build
make docker-build
```

Go 版本要求：1.26+（来自 CLIProxyAPI SDK 依赖）
