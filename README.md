# Warden - AI Gateway

Warden 是一个轻量级 AI Gateway，提供统一的 OpenAI 兼容接口，将请求路由到多个上游 LLM Provider（OpenAI、Anthropic、Ollama、Qwen、GitHub Copilot），并透明注入 MCP 工具。

## 核心功能

- **多协议适配** — 统一 OpenAI 格式入口，自动转换 Anthropic/Ollama/Qwen/Copilot 协议
- **Provider 路由与容错** — 按前缀路由，配置顺序决定优先级，失败自动指数退避抑制，支持多 Provider Failover；手动抑制的 provider 会在常规选择与 failover 中被跳过
- **MCP 工具注入** — 自动注入 MCP 工具到请求，拦截并执行工具调用后继续对话，支持多轮递归和混合工具调用
- **MCP 工具 Hook** — 支持 exec/ai/http 类型的 pre/post hook，可对工具调用进行安全审查或增强
- **模型别名** — Provider 级别的模型别名映射，别名在 `/models` 中可见，请求时自动解析为真实模型名
- **模型发现** — 自动查询上游 `/models` 端点，聚合多 Provider 模型列表，按模型名智能路由
- **System Prompt 注入** — 按路由和模型精确匹配，自动注入自定义 system 提示词
- **请求日志** — 支持文件和 HTTP 双后端，记录完整请求/响应和工具调用步骤，HTTP 后端支持 Go 模板渲染
- **Web 管理面板** — 内置 Web UI，实时监控 Provider 状态、查看请求日志流、在线编辑配置、MCP 工具调试；监控卡片优先展示用量/速率/出错与退化压力，支持 5s 采样的用量/错误折线趋势，并提供 P95 TTFT、P99 Throughput 组合热点定位性能问题
- **SSH 远程 MCP** — MCP Server 可通过 SSH 在远程主机执行，自动继承 `~/.ssh/config`
- **请求补丁** — Provider 级别的 JSON Patch (RFC 6902)，可在转发前修改请求体
- **Prometheus 指标** — 请求计数、延迟、TTFT、Throughput、Token 用量、Provider 健康状态（支持 route/provider/model 维度）
- **同时支持 Chat Completions 和 Responses API**
- **流式响应支持** — 包括流式场景下的工具调用拦截

## 快速开始

### 构建

```bash
make build        # 前端构建 + Go 编译，输出到 bin/warden
make test         # go vet + go test
make web          # 仅构建前端
```

### 运行

```bash
./bin/warden                     # 默认启动
./bin/warden -c /path/to.yaml    # 指定配置文件
./bin/warden -i                  # 安装为 systemd 服务
./bin/warden -r                  # 向运行中的实例发送 SIGHUP 热重载
```

配置文件搜索顺序：`warden.yaml` → `config/warden.yaml` → `/etc/warden.yaml`（同时支持 `.yml` 后缀）

### 配置

创建 `warden.yaml`（完整示例参见 [`config/warden.example.yaml`](config/warden.example.yaml)）：

```yaml
addr: ":8080"
# admin_password: "your-secret-password"

provider:
    openai:
        url: "https://api.openai.com/v1"
        protocol: "openai"
        api_key: "${OPENAI_API_KEY}"
        timeout: "60s"

    anthropic:
        url: "https://api.anthropic.com/v1"
        protocol: "anthropic"
        api_key: "${ANTHROPIC_API_KEY}"
        timeout: "60s"

    ollama:
        url: "http://localhost:11434/v1"
        protocol: "ollama"
        timeout: "300s"

route:
    /openai:
        providers: ["openai"]
        tools: ["web-search"]

    /anthropic:
        providers: ["anthropic"]
        tools: ["web-search", "filesystem"]

mcp:
    web-search:
        command: "npx"
        args: ["-y", "@anthropic/mcp-server-web-search"]
        env:
            ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"

    filesystem:
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem"]
```

支持的 Provider 协议：`openai`、`anthropic`、`ollama`、`qwen`（OAuth 自动刷新）、`copilot`（GitHub Copilot OAuth）。API Key 支持 `${ENV_VAR}` 环境变量展开。

### Web 管理面板

设置 `admin_password` 后，访问 `http://localhost:8080/_admin/`（用户名 `admin`）。

- Dashboard — Provider 健康状态、路由概览、MCP 连接状态、Tokens（按 provider 聚合的模型输出 token/s）
- Providers — 详情、模型列表、探活
- Routes — 关联 Provider 统计、MCP 工具状态、请求测试
- MCP Tools — 工具详情、参数调试、enable/disable 开关
- Tool Hooks — 全局 hook 规则管理
- Logs — SSE 实时请求日志流
- Config — 结构化在线编辑、验证、重启

## 使用

Warden 提供 OpenAI 兼容接口，路由前缀替代原始 `/v1`：

```bash
# Chat Completions
curl http://localhost:8080/openai/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o", "messages": [{"role": "user", "content": "Hello"}]}'

# Responses API
curl http://localhost:8080/openai/responses \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o", "input": "Hello"}'

# 查看可用模型（聚合所有 Provider）
curl http://localhost:8080/openai/models
```

MCP 工具对客户端完全透明 — Warden 自动注入工具定义、拦截工具调用、执行后将结果回传 LLM 继续生成。客户端只看到最终回复（或仅看到客户端自己定义的工具调用）。

所有上游转发请求（包括 `chat/completions`、`responses`、透明代理）统一采用 header 安全转发策略：复制请求头后清洗 hop-by-hop/客户端认证头，重建 `X-Forwarded-*`，最后注入 provider 认证头。

Anthropic 原生 `/messages` 端点也可通过路由前缀访问，但走透明代理路径，**不支持 MCP 工具注入和 System Prompt 注入**：

透明代理会先复制客户端请求头，再清洗 hop-by-hop/客户端认证头并重建 `X-Forwarded-*`，最后注入 provider 认证头。

```bash
# Anthropic native Messages API（透明代理）
curl http://localhost:8080/anthropic/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"model": "claude-opus-4-6", "max_tokens": 1024, "messages": [{"role": "user", "content": "Hello"}]}'
```

如需 MCP 工具注入，改用 `chat/completions` 端点以 OpenAI 格式访问 Anthropic provider，网关自动完成协议转换。

## 项目结构

```
cmd/warden/          # 入口
config/              # 配置定义、验证、示例
web/admin/           # Vue 3 前端（embed 到二进制）
internal/
  gateway/           # 核心网关：路由、选择器、协议适配、工具注入/执行、Admin API、Prometheus 指标
  mcp/               # MCP 客户端（JSON-RPC stdio）
  reqlog/            # 请求日志（文件/HTTP 双后端）、SSE 广播器
  install/           # systemd 服务安装
pkg/
  protocol/          # LLM 协议公共类型（Event、ToolCallInfo、StreamParser）
    openai/          # OpenAI 类型定义、流式解析、工具/提示词注入
    anthropic/       # Anthropic 协议转换、流式解析、认证
  provider/          # OAuth token 管理（Qwen、GitHub Copilot）
  toolhook/          # 通用 Tool Hook 执行（适用于任意 tool call）
  ssh/               # SSH 远程执行
```

## License

MPL-2.0
