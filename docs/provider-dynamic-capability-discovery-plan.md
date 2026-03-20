# Provider 动态能力与单协议 Route 方案

> 更新日期：2026-03-19

本文只记录当前已经落地的方案，不再保留旧的多协议 route 设计。

## 1. 外部协议面结论

基于 2026-03-19 查询到的官方资料，warden 当前固定采用以下 provider family 候选协议面：

- `openai`:
  - `chat`
  - `responses_stateless`
  - `responses_stateful`
- `anthropic`:
  - `chat`
  - `anthropic`
- `qwen`:
  - `chat`
- `copilot`:
  - `chat`
- `ollama`:
  - `chat`

对实现最关键的决策：

- `qwen` 在 warden 中只按 `chat` 处理
- `copilot` 不再默认支持任何 `responses*`
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

provider 卡片展示三类信息：

- `candidate_protocols`：按 provider family 推导的候选协议面
- `display_protocols`：轻量探测后的展示结果
- `provider + model + protocol` 精确 probe 结果

这些结果：

- 可以不精确
- 会影响 UI 展示和配置提示
- 不直接决定运行时请求是否路由

### 3.2 路由层

真正决定运行时的是：

1. `route.protocol`
2. `route.exact_models` / `route.wildcard_models`
3. 静态 provider family 兼容校验（`provider.family` 必填，`provider.protocol` 仅作兼容别名）

补充说明：

- `provider.family` 是必填字段，`provider.protocol` 只作为兼容别名保留
- provider family 先给出候选协议面，再由 `enabled_protocols` / `disabled_protocols` 做静态收缩
- 这些 provider 级收缩规则不会替代 `route.protocol`

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
  - 轻量协议检测
  - 精确 `provider + model + protocol` 探测
- 从 provider 卡片新建 route 时，只预选单个协议，不再生成 model-level 多协议结构

## 6. 已完成项

- 后端配置结构改为单协议 route
- selector / gateway / admin API 全部改为基于 `route.protocol`
- 管理端 route 编辑器改为单协议模型编辑
- 示例配置、系统配置、核心文档同步更新
