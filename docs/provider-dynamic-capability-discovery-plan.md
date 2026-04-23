# Provider 动态能力与单协议 Route 方案

> 更新日期：2026-04-18
>
> 状态：current
>
> 文件名保留了历史上的 `plan`，但本文只记录当前实现。

本文只记录当前已经落地的方案，不再保留旧的多协议 route 设计。

## 1. 外部协议面结论

当前实现不是“在线查询官方资料后动态决定能力”，而是先按 `provider.family` 做静态能力推导，再叠加管理端展示层 probe。

当前 provider family 候选协议面是：

- `openai`:
  - `chat`
  - `responses_stateless`
  - `responses_stateful`
  - `anthropic`（仅当该 provider 开启 `anthropic_to_chat`）
- `anthropic`:
  - `chat`
  - `anthropic`
- `qwen`:
  - `chat`
- `copilot`:
  - `chat`

对实现最关键的决策：

- `qwen` 在 warden 中只按 `chat` 处理
- `copilot` 不再默认支持任何 `responses*`
- OpenAI-compatible 第三方上游（例如 Ollama）不再拥有独立 family，而是归入 `openai`；若只支持聊天接口，必须显式配置 `service_protocols: [chat]`
- `openai` provider 的 `anthropic` 能力不是原生 `/messages`，而是 `anthropic_to_chat` 的受控桥接能力
- provider 级轻量探测只用于展示，不作为路由运行时真相

## 2. 路由结构

每个 route 必须先锁定唯一协议：

```yaml
route:
  /openai:
    protocol: chat
    exact_models:
      gpt-4o:
        upstreams:
          - provider: openai
            model: gpt-4o
    wildcard_models:
      "gpt-*":
        providers:
          - openai
```

约束：

- `route.protocol` 必填，且只允许一个值：
  - `chat`
  - `responses_stateless`
  - `responses_stateful`
  - `anthropic`
- `exact_models` 不再嵌套 `protocols`
- `wildcard_models` 不再嵌套 `protocols`
- route 内所有模型映射都受 `route.protocol` 统一约束

## 3. 运行时真相来源

真相分两层，不允许再混淆：

### 3.1 展示层

provider 详情页当前展示四类信息：

- `candidate_protocols`：按 provider family 推导的候选协议面
- `configured_protocols`：当前配置下真正允许声明 route 的协议面
- `display_protocols`：轻量探测后的展示结果
- `provider + model + protocol` 精确 probe 结果

这些结果：

- 可以不精确，尤其 `display_protocols` 只是 endpoint 可达性提示
- 会影响 UI 展示和配置提示
- 不直接决定运行时请求是否路由

补充边界：

- `candidate_protocols` / `configured_protocols` 来自本地静态规则，不依赖启动期网络
- `display_protocols` 来自轻量 `OPTIONS` 探测，更多是“这个 endpoint 看起来是否可达”的提示
- 对桥接能力，精确 probe 才会走真实请求路径；例如 `anthropic_to_chat` 的 anthropic probe 会先把 Messages 请求转换成上游 Chat 请求再探测

### 3.2 路由层

真正决定运行时的是：

1. `route.protocol`
2. `route.exact_models` / `route.wildcard_models`
3. 静态 provider family 兼容校验（`provider.family` 必填，`provider.protocol` 仅作兼容别名）

补充说明：

- `provider.family` 是必填字段，`provider.protocol` 只作为兼容别名保留
- provider family 直接决定候选协议面

也就是说：

- route 能暴露什么入口，只看 `route.protocol`
- 某个 public model 指向哪些上游，只看该 route model 配置
- provider 卡片上的展示协议不会改变 selector / gateway 的选择结果
- 配置校验不依赖启动期网络；provider 级探测依然只属于展示层

## 4. responses 约束

- `responses_stateless`：
  - 只接受无状态 `/responses`
  - 明确拒绝 `previous_response_id`
- `responses_stateful`：
  - 同时接受无状态和有状态 `/responses`
  - exact model 只允许单 upstream
  - wildcard model 只允许单 provider
  - 有状态请求禁用 failover

## 5. 管理端行为

- `Routes` 页面先选 route 唯一协议，再编辑模型
- route 切换为 `responses_stateful` 时，UI 会把 exact upstream / wildcard provider 收敛到单个
- `Providers` 页面可触发：
  - 轻量协议检测（更新 `display_protocols`）
  - 精确 `provider + model + protocol` 探测
- 从 provider 卡片新建 route 时，只预选单个协议，不再生成 model-level 多协议结构
- 这个预选值取自该 provider 当前 `configured_protocols` 的首个协议，而不是 `display_protocols`

## 6. 已完成项

- 后端配置结构改为单协议 route
- selector / gateway / admin API 全部改为基于 `route.protocol`
- 管理端 route 编辑器改为单协议模型编辑
- 示例配置、系统配置、核心文档同步更新
