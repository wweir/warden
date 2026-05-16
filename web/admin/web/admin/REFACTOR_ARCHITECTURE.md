# Provider 详情页重构架构对比

## 重构前架构

```
ProviderDetail.vue (2387 行)
├── 30+ ref 变量
├── 20+ computed 属性
├── 40+ 方法
├── 多个 watch 监听器
└── 所有逻辑混在一起
```

## 重构后架构

```
ProviderDetail.vue (450 行)
├── 使用 3 个 composables
├── 引入 5 个子组件
├── 10 个 ref 变量
├── 5 个 computed 属性
└── 5 个方法

Composables:
├── useProviderForm.ts (表单状态管理)
├── useCliproxyAuth.ts (认证管理)
└── useProviderRuntime.ts (运行时工具)

Components:
├── ProviderBasicForm.vue (基础表单)
├── ProviderModelsEditor.vue (模型编辑)
├── ProviderAdvancedSettings.vue (高级设置)
├── CLIProxyAuthManager.vue (认证管理)
└── ProviderRuntimeTools.vue (运行时工具)

Utils:
├── providerFormatters.ts (格式化函数)
└── providerHelpers.ts (辅助函数)
```

## 职责分离

### 原架构问题
- 单一文件包含所有逻辑
- 状态管理混乱
- 难以测试和维护
- 代码复用困难

### 新架构优势
- 每个模块职责单一
- 状态管理清晰
- 易于单元测试
- 逻辑可复用

## 性能优化

### 减少 watch 监听
- 原：5+ 个 watch，监听多个字段
- 新：composables 内部管理，减少不必要的触发

### 计算属性优化
- 原：20+ 个计算属性，部分重复计算
- 新：5 个核心计算属性，逻辑下沉到 composables

### 组件懒加载
- 运行时工具仅在非创建模式下加载
- CLIProxy 认证仅在需要时渲染

## 可维护性提升

### 代码定位
- 原：在 2387 行中查找逻辑
- 新：根据功能直接定位到对应文件

### 修改影响范围
- 原：修改可能影响整个组件
- 新：修改仅影响单个模块

### 测试覆盖
- 原：难以编写单元测试
- 新：每个 composable 和组件可独立测试

## 复用性

### Composables 复用
```typescript
// 在其他页面使用表单逻辑
import { useProviderForm } from '@/composables/useProviderForm'

// 在其他页面使用认证管理
import { useCliproxyAuth } from '@/composables/useCliproxyAuth'
```

### 组件复用
```vue
<!-- 在其他页面使用模型编辑器 -->
<ProviderModelsEditor
  v-model="models"
  :discovered-model-ids="discoveredIds"
/>
```

## 迁移路径

### 阶段1：验证功能（当前）
- 运行测试清单
- 确保所有功能正常

### 阶段2：性能测试
- 对比加载时间
- 对比内存占用
- 对比交互响应

### 阶段3：清理
- 删除备份文件
- 更新相关文档
- 提交代码

## 未来扩展

### 易于添加新功能
- 新增 provider 类型：只需扩展 preset 配置
- 新增认证方式：在 ProviderBasicForm 中添加
- 新增运行时工具：在 ProviderRuntimeTools 中添加

### 易于优化
- 表单验证：在 useProviderForm 中统一处理
- 错误处理：在 composables 中统一管理
- 缓存策略：在 composables 中实现
