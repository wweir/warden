# Cross-Platform Deployment

这个文档只回答一个问题：Warden 现在如何在 Linux、macOS、Windows 上运行和托管。

不要把“能交叉编译”误认为“部署语义已经对齐”。这两件事不是一回事。

## Current Support Matrix

| Capability | Linux | macOS | Windows |
| --- | --- | --- | --- |
| Build binary | Yes | Yes | Yes |
| Run in foreground | Yes | Yes | Yes |
| Built-in managed install (`warden -i`) | Yes (`systemd`) | Yes (`launchd`) | Yes (Task Scheduler) |
| Built-in CLI reload (`warden -r`) | Yes | Yes | No |
| Recommended long-running supervisor | `systemd` | `launchd` | Task Scheduler |

当前代码事实：

- `cmd/warden` 的主运行路径是标准 HTTP 服务，没有 Linux 专属协议依赖，因此前台运行是跨平台的
- `internal/install` 已按平台接入 `systemd`、`launchd`、Task Scheduler
- Windows 终端用户入口默认是单独的 self-extracting `setup.exe`，不是裸 `warden.exe`
- 托管安装总是把二进制落到平台标准路径；若目标配置文件已存在，安装前会先校验该配置
- 管理台触发的重启在 Unix 上走原地重启，在 Windows 前台进程上走“拉起新进程再退出旧进程”；被任务包装脚本托管时则通过受控退出码请求包装脚本立即拉起新实例
- `warden -r` 仍然依赖 Unix 信号模型；在 Windows 上不应该作为运维接口使用

## Recommended Model

应该把部署支持拆成三层，而不是试图用一个开关覆盖三种 OS：

1. Binary/runtime layer

- 三个平台统一使用相同配置模型和 HTTP API
- 构建产物只区分文件扩展名和平台归档格式

2. Supervisor layer

- Linux 使用 `systemd`
- macOS 使用 `launchd`
- Windows 使用 Task Scheduler

3. Operations layer

- 启动、停止、重启交给平台 supervisor
- Warden 自身只负责优雅关闭、配置校验、HTTP 服务和管理面

这个分层的原因很直接：进程托管语义是 OS 责任，不是网关业务责任。硬把 `systemd` 心智复制到 Windows，只会得到一套名义统一、实际失效的伪抽象。

## Linux

Linux 是当前一等支持平台。

推荐方式：

- 使用 `make build` 或 `make package`
- 使用 `make install` 或 `sudo ./bin/warden -i` 安装到 `systemd`
- 配置文件使用 `/etc/warden.yaml`
- 首次安装只会在配置不存在时生成最小 bootstrap 配置；交互安装会明确询问是否对外提供服务。默认只监听 `127.0.0.1:9832`，后台入口为 `http://localhost:9832/_admin/`，并启用用户名 `admin`、密码 `admin` 的本机管理后台；若选择对外提供服务，则监听 `:9832`，但不写入 `admin_password`，管理后台保持禁用直到手动设置强密码
- `make install` 会调用 `warden -i -y`，跳过安装确认并启动或重启服务；`-y` 不会隐式选择对外监听
- bootstrap 配置默认不写入 provider / route，避免服务启动依赖外部账号、网络或 OAuth 凭证

运维方式：

- 启动/停止/重启：`systemctl start|stop|restart warden`
- CLI 重载：`warden -r`
- 管理台保存配置后触发重载

## macOS

macOS 的正确支持方式是：

- 前台运行：直接执行二进制
- 后台托管：使用内置 `warden -i` 生成并安装 `launchd` 配置

推荐目录：

- Binary: `/usr/local/bin/warden` 或 `/opt/homebrew/bin/warden`
- Config: `/usr/local/etc/warden.yaml`
- Logs: `/usr/local/var/log/warden.log`

内置安装器会做这些事：

- 把 `LaunchDaemon` 写到 `/Library/LaunchDaemons/com.wweir.warden.plist`
- 配置 `RunAtLoad=true`
- 配置 `KeepAlive=true`
- 把 stdout/stderr 重定向到固定日志文件
- 首次安装只会在配置不存在时生成最小 bootstrap 配置；交互安装会明确询问是否对外提供服务。默认只监听 `127.0.0.1:9832`，后台入口为 `http://localhost:9832/_admin/`，并启用用户名 `admin`、密码 `admin` 的本机管理后台；若选择对外提供服务，则监听 `:9832`，但不写入 `admin_password`，管理后台保持禁用直到手动设置强密码
- `-y` 可用于跳过安装确认并启动或重启托管服务；它不隐式选择对外监听
- bootstrap 配置默认不写入 provider / route

推荐操作：

- 首次部署：`sudo make install` 或 `sudo ./warden -i`
- 配置变更：优先通过管理台触发重启，或由 `launchctl kickstart -k` 重启
- CLI `-r` 仍是 Unix 信号路径，可继续使用

## Windows

Windows 要区分开发运行和生产托管。

开发运行：

- 直接执行 `warden.exe -c C:\path\to\warden.yaml`

生产托管：

- 对终端用户：双击 `setup.exe`
- 对开发/调试：也可以在提升权限的终端中执行 `make install` 或 `warden.exe -i`

推荐目录：

- Binary: `C:\Program Files\Warden\warden.exe`
- Config: `C:\ProgramData\Warden\warden.yaml`
- Logs: `C:\ProgramData\Warden\logs\warden.log`

内置安装器会做这些事：

- Trigger: `At startup`
- User: `SYSTEM`
- Action: 通过任务包装脚本启动 `warden.exe -c ...`
- 包装脚本在受控重启退出码上立即拉起新实例；只在异常退出后延迟 5 秒重试
- stdout/stderr: 重定向到固定日志文件
- 首次安装只会在配置不存在时生成最小 bootstrap 配置；交互安装会明确询问是否对外提供服务。默认只监听 `127.0.0.1:9832`，后台入口为 `http://localhost:9832/_admin/`，并启用用户名 `admin`、密码 `admin` 的本机管理后台；若选择对外提供服务，则监听 `:9832`，但不写入 `admin_password`，管理后台保持禁用直到手动设置强密码
- `-y` 可用于跳过安装确认并启动或重启托管任务；它不隐式选择对外监听
- bootstrap 配置默认不写入 provider / route

边界说明：

- 当前仓库没有内置 Windows Service SCM 集成，选择的是 Task Scheduler 路线
- 当前仓库的 `warden -r` 不应视为 Windows 运维能力
- Windows 上的配置应用重启由当前进程自拉起新实例或请求包装脚本立即重拉，不走 Unix 信号模型

## Packaging Guidance

发布层面的建议不应继续只按 Linux 习惯处理：

- Linux/macOS：`tar.gz`
- Windows：默认分发 self-extracting `setup.exe`
- 每个归档至少包含：二进制、示例配置、平台部署说明

如果需要调试或保留原始运行时包，可以用 `WINDOWS_PACKAGE_FORMAT=zip make package` 退回裸运行时 zip。

如果只发一个裸二进制，你是在把部署成本外包给操作者，不是在做“支持”。

## What "Complete Support" Actually Means

如果要把“支持 macOS / Windows”说完整，最低标准应当是：

- 能提供对应平台的前台运行命令
- 能说明推荐的后台托管器
- 能说明配置文件、日志文件、二进制的推荐落点
- 能说明升级、重启、日志查看的运维路径
- 明确哪些 CLI 入口只在 Linux/Unix 有效

在这个标准下，当前仓库是：

- Linux：完整
- macOS：完整
- Windows：完整，但托管模型明确选择 Task Scheduler 而不是 SCM Service
