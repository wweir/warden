# Warden

Warden 是一个给多种 AI 模型做“统一入口”的网关。

如果你手上同时有 OpenAI、Anthropic、Qwen、Copilot、Ollama 等不同来源的模型，Warden 可以把它们收在同一个服务后面。你的客户端只需要对接一个地址，后面具体走哪个 provider、哪个模型、失败后切到哪里，都由 Warden 处理。

一句话理解：

- 对客户端：它像一个统一的 AI 接口
- 对管理员：它像一个可以切换上游、看状态、看日志的控制台

文档入口：

- 架构说明：[ARCHITECTURE.md](/home/wweir/Mine/warden/ARCHITECTURE.md)
- 专题文档索引：[docs/README.md](/home/wweir/Mine/warden/docs/README.md)
- 配置说明：[config/README.md](/home/wweir/Mine/warden/config/README.md)

## 它能解决什么问题

很多团队在接入 AI 时会遇到几个实际问题：

- 不同模型供应商的接口地址、鉴权方式、协议细节不完全一样
- 你想给不同业务走不同模型，但不想让客户端写死很多分支
- 某个上游不稳定时，希望自动切换到备选 provider
- 想知道现在请求都打到哪里了、哪个 provider 挂了、哪些 key 用量高
- 想在模型调用工具时增加额外的审计或回调

Warden 就是为这些问题准备的。

## 你可以把它当成什么

- 一个统一的 API 入口
- 一个模型路由器
- 一个多 provider 兜底层
- 一个带管理后台的 AI 网关

## 当前主要能力

- 统一接入 `openai`、`anthropic`、`ollama`、`qwen`、`copilot`
- 用路由规则决定“哪个入口暴露哪些模型”
- 一个公开模型可以对应多个上游，按顺序自动 failover
- 支持 OpenAI `chat/completions`、`responses`，以及 Anthropic `messages`
- 提供管理后台，可查看 provider、route、日志和配置
- 支持客户端 API Key 鉴权和按 key 统计
- 支持工具调用 Hook：`exec`、`ai`、`http`
- 提供请求日志、SSE 日志流和 Prometheus 指标

## 不再支持

下面这些已经不是当前产品边界，不要按旧文档理解：

- 内置 MCP client 运行时
- SSH 远程执行与 SSH 配置块

如果你还看到旧配置或旧截图里有 `mcp` / `ssh`，那是历史残留。

## 管理后台长什么样

后台入口默认是 `http://localhost:8080/_admin/`，用户名固定为 `admin`。

下面是当前实例的后台截图：

![Warden Admin Dashboard](docs/assets/admin-dashboard.png)

![Warden Admin Providers](docs/assets/admin-providers.png)

![Warden Admin Routes](docs/assets/admin-routes.png)

## 最少步骤跑起来

如果你只想先把服务跑起来，按下面做就够了。

### 1. 构建

```bash
make build
```

这会：

- 构建前端管理页面
- 编译 Go 程序
- 输出 `bin/warden`
- 通过 `ldflags` 注入版本和构建日期

### 2. 准备配置

完整示例见 [config/warden.example.yaml](/home/wweir/Mine/warden/config/warden.example.yaml)。

最小示例：

```yaml
addr: ":8080"
admin_password: "admin"

provider:
  openai:
    family: "openai"
    url: "https://api.openai.com/v1"
    api_key: "${OPENAI_API_KEY}"
    timeout: "60s"

route:
  /openai:
    protocol: chat
    exact_models:
      gpt-4o:
        upstreams:
          - provider: "openai"
            model: "gpt-4o"
```

### 3. 启动

```bash
./bin/warden
```

也可以手动指定配置文件：

```bash
./bin/warden -c /path/to/warden.yaml
```

配置文件搜索顺序：

- `warden.yaml`
- `config/warden.yaml`
- `/etc/warden.yaml`

同时支持 `.yml` 后缀。

### 4. 访问后台

启动后打开：

```text
http://localhost:8080/_admin/
```

- 用户名固定为 `admin`
- 密码来自配置里的 `admin_password`

## 给客户端怎么用

客户端不需要知道你后面挂了几个 provider，只要打 Warden 暴露的统一入口。

示例：

```bash
curl http://localhost:8080/openai/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

如果你走 OpenAI Responses API：

```bash
curl http://localhost:8080/openai/responses \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o","input":"Hello"}'
```

查看模型列表：

```bash
curl http://localhost:8080/openai/models
```

如果你配置了客户端 API Key，需要额外带鉴权头：

```bash
curl http://localhost:8080/openai/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer wk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx" \
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'
```

## 管理后台里能做什么

- `Dashboard`：看 provider 状态、路由概览、实时指标
- `Providers`：看每个 provider 的健康状态、模型和配置
- `Routes`：管理“对外暴露什么模型，实际走哪些上游”
- `Tool Hooks`：配置模型工具调用后的附加动作
- `Logs`：实时看请求日志
- `Config`：在线查看和编辑配置

## 配置时最重要的理解

非技术读者最容易卡在这里，心智模型先记住这三个词：

- `provider`：你真实连接的上游 AI 服务
- `route`：你对外暴露的访问入口
- `model`：客户端看到的模型名，背后可以映射到真实上游模型

例如：

- 客户端请求 `/openai`
- 请求里写 `model=gpt-4o`
- Warden 决定它实际转发到哪个 provider
- 如果第一个 provider 失败，可以切到下一个

## 配置要点

- `provider.*.url` 必须是完整的 `http/https` 地址
- `provider.*.family` 必填
- `route.protocol` 必须显式声明，每个 route 只能有一种协议
- `route.exact_models` 适合精确声明模型
- `route.wildcard_models` 适合做通配规则
- `api_keys` 为空时，网关不校验客户端 API Key
- `admin_password`、`api_keys`、`provider.*.api_key` 读取时兼容明文和 base64，写回配置时统一写为 base64

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

## 什么时候看其它文档

- 你想理解内部分层和设计边界：看 [ARCHITECTURE.md](/home/wweir/Mine/warden/ARCHITECTURE.md)
- 你想精确理解配置字段和校验规则：看 [config/README.md](/home/wweir/Mine/warden/config/README.md)
- 你要看某个专题的限制或方案：看 [docs/README.md](/home/wweir/Mine/warden/docs/README.md)

## License

MPL-2.0
