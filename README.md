# Warden - AI Gateway

Warden 是一个轻量级 AI Gateway，提供统一的 OpenAI 兼容接口，将请求路由到多个上游 LLM Provider（OpenAI、Anthropic、Ollama、通义千问），并透明注入 MCP 工具。

## 核心功能

- **多协议适配** — 统一 OpenAI 格式入口，自动转换 Anthropic/Ollama/Qwen 协议
- **Provider 路由与容错** — 按前缀路由，配置顺序决定优先级，失败自动指数退避抑制
- **MCP 工具注入** — 自动注入 MCP 工具到请求，拦截并执行工具调用后继续对话，支持多轮递归
- **模型别名** — Provider 级别的模型别名映射，别名在 `/models` 中可见，请求时自动解析为真实模型名
- **System Prompt 注入** — 按路由和模型精确匹配，自动在请求最前面注入自定义 system 提示词
- **请求日志** — 可选的 JSON 文件日志，记录完整请求/响应和工具调用步骤
- **Web 管理面板** — 内置 Web UI，实时监控 Provider 状态、查看请求日志流、在线编辑配置
- **交互模式** — 终端交互菜单，动态开关 MCP 工具
- **同时支持 Chat Completions 和 Responses API**
- **流式响应支持** — 包括流式场景下的工具调用拦截

## 快速开始

### 构建

```bash
make build        # 前端构建 + Go 编译，输出到 bin/warden
make test         # go vet + go test
make web          # 仅构建前端
```

### 配置

创建 `warden.yaml`（参考 `config/warden.example.yaml`）：

```yaml
addr: ":8080"

# Web 管理面板密码（留空则不启用）
# admin_password: "your-secret-password"

# 请求日志（可选）
# log:
#   file_dir: "./logs"

# Provider 配置
provider:
  openai:
    url: "https://api.openai.com/v1"
    protocol: "openai"
    api_key: "${OPENAI_API_KEY}"
    timeout: "60s"
    # proxy: "socks5://127.0.0.1:1080"

  anthropic:
    url: "https://api.anthropic.com/v1"
    protocol: "anthropic"
    api_key: "${ANTHROPIC_API_KEY}"
    timeout: "120s"

  ollama:
    url: "http://localhost:11434/v1"
    protocol: "ollama"
    timeout: "300s"
    # model_aliases:
    #   gpt-4o: "llama3.3"

# 路由配置 — providers 顺序即优先级
route:
  /openai:
    providers: ["openai"]
    tools: ["web-search"]

  /anthropic:
    providers: ["anthropic"]
    tools: ["web-search", "filesystem"]
    # system_prompts:
    #   claude-sonnet-4-5-20250929: "You are a helpful assistant."

# MCP 工具配置
mcp:
  web-search:
    command: "npx"
    args: ["-y", "@anthropic/mcp-server-web-search"]
    env:
      ANTHROPIC_API_KEY: "${ANTHROPIC_API_KEY}"

  filesystem:
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-filesystem"]
    env: {}
```

### 运行

```bash
./bin/warden                     # 普通模式
./bin/warden -c /path/to.yaml    # 指定配置文件
```

配置文件搜索顺序：`warden.yaml` → `config/warden.yaml` → `/etc/warden.yaml`

### Web 管理面板

设置 `admin_password` 后，访问 `http://localhost:8080/_admin/`，使用 Basic Auth 登录（用户名 `admin`）。

功能：
- **Dashboard** — Provider 健康状态、路由概览、MCP 连接状态
- **Logs** — SSE 实时请求日志流
- **Config** — 在线查看和编辑配置

## 使用

Warden 提供 OpenAI 兼容接口，路由前缀替代原始 `/v1`：

```bash
# Chat Completions
curl http://localhost:8080/openai/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'

# Responses API
curl http://localhost:8080/openai/responses \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "input": "Hello"
  }'

# 查看可用模型（聚合所有 Provider）
curl http://localhost:8080/openai/models
```

MCP 工具对客户端完全透明 — Warden 自动注入工具定义、拦截工具调用、执行后将结果回传 LLM 继续生成。客户端只看到最终回复（或仅看到客户端自己定义的工具调用）。

## 项目结构

```
cmd/warden/          # 入口
config/              # 配置定义、验证、示例
web/
  admin/             # Vue 3 前端（embed 到二进制）
internal/
  app/               # 应用生命周期、交互模式
  gateway/           # 核心网关：路由、选择器、协议适配、工具注入、Admin API
  mcp/               # MCP 客户端
  reqlog/            # 请求/响应日志、SSE 广播器
  install/           # systemd 服务安装
pkg/
  anthropic/         # Anthropic 协议转换
  openai/            # OpenAI 类型定义
  copilot/           # GitHub Copilot OAuth
  qwen/              # Qwen OAuth
  ssh/               # SSH 远程执行
  sse/               # SSE 解析
```

## License

MPL-2.0
