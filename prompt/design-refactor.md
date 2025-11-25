# 任务：Genesis 组件设计文档审查与重构

## 输入

请阅读以下文档：

1. **规范文档（必读）**：
   - `docs/refactoring-plan.md` — 重构执行计划（source-of-truth）
   - `docs/genesis-design.md` — Genesis 整体设计
   - `docs/specs/component-spec.md` — 组件开发规范
   - `docs/reviews/architecture-decisions.md` — 架构决策记录

2. **待审查组件文档**：
   - `docs/{component}-design.md`（如果存在）

## 任务

对 `{component}` 组件的设计文档进行审查，打印输出偏差报告。

### 输出格式

请输出以下格式的审查报告：

```markdown
# {Component} 设计文档审查报告

## 1. 文档存在性
- [ ] 设计文档是否存在
- [ ] 文档位置：`docs/{component}-design.md`

## 2. 层级归属
- 当前层级：{L0/L1/L2/L3/Glue}
- 应遵循策略：{扁平化/保留 internal}

## 3. 目录结构规范对比
| 规范要求 | 当前状态 | 是否符合 | 修改建议 |
|---------|---------|---------|---------|
| types/ 扁平化（L2/L3） | | | |
| internal/ 保留（L1） | | | |
| options.go 存在 | | | |

## 4. 接口设计对比
| 规范要求 | 当前状态 | 是否符合 | 修改建议 |
|---------|---------|---------|---------|
| New() 签名符合规范 | | | |
| WithLogger Option | | | |
| WithMeter Option | | | |
| WithTracer Option | | | |
| Lifecycle 接口（如需要） | | | |

## 5. 缺失内容
- [ ] 需要补充的设计说明
- [ ] 需要新增的章节

## 6. 总结与优先级
- P0（阻塞性问题）：
- P1（重要问题）：
- P2（建议改进）：

### 注意事项

- 仅审查设计文档，不涉及代码
- 以 `refactoring-plan.md` 为准
- 明确标注偏差的严重程度
```

## 输入

请阅读以下文档：
1. **规范文档（必读）**：
   - `docs/refactoring-plan.md` — 重构执行计划
   - `docs/specs/component-spec.md` — 组件开发规范

2. **审查报告**：
   - 上一步生成的 `{component}` 设计审查报告

3. **现有设计文档**（如存在）：
   - `docs/{component}-design.md`

### 任务
根据审查报告，修订或新建 `{component}` 的设计文档，使其符合规范。

### 输出要求

设计文档应包含以下章节：

# {Component} 设计文档

## 1. 概述
- 组件职责
- 所属层级（L0/L1/L2/L3/Glue）
- 核心能力

## 2. 目录结构
```text
pkg/{component}/
├── {component}.go    # 接口定义、Config、Errors、Factory
├── options.go        # Option 模式
└── ...
internal/{component}/ # （仅 L1 保留）
└── ...
```

## 3. 接口定义

```go
// Interface 定义核心接口
type Interface interface {
    // 方法列表
}

// Config 配置结构
type Config struct {
    // 字段列表
}
```

## 4. 工厂函数

```go
// New 创建实例
// 参数说明...
func New(dep Dependency, opts ...Option) (Interface, error)
```

## 5. Option 定义

```go
func WithLogger(l clog.Logger) Option
func WithMeter(m metric.Meter) Option
func WithTracer(t trace.Tracer) Option
```

## 6. 生命周期（如适用）

- Start(ctx) 行为
- Stop(ctx) 行为

## 7. 使用示例

简单的使用示例，详细的版本需要参照 examples 文件夹中的示例。

```go
// 独立模式示例
// 容器模式示例
```

## 8. 错误处理

- Sentinel Errors 定义

### 注意事项

- 确保与 `refactoring-plan.md` 的层级策略一致
- L2/L3 组件必须扁平化 types/
- 接口优先，实现细节放 internal/（L1）或非导出类型（L2/L3）

```
