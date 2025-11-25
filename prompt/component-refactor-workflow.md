# Genesis 组件重构工作流程提示词

本文档包含一套用于指导 AI 进行组件重构的结构化提示词。按照顺序执行：设计审查 → 设计修订 → 代码审查 → 代码重构。

---

## 提示词 1：设计文档审查 (Design Review)

用途：审查组件现有设计文档，对比规范，生成设计偏差报告。

## 任务：Genesis 组件设计文档审查

### 输入
请阅读以下文档：
1. **规范文档（必读）**：
   - `docs/refactoring-plan.md` — 重构执行计划（source-of-truth）
   - `docs/genesis-design.md` — Genesis 整体设计
   - `docs/specs/component-spec.md` — 组件开发规范
   - `docs/reviews/architecture-decisions.md` — 架构决策记录

2. **待审查组件文档**：
   - `docs/{component}-design.md`（如果存在）

### 任务
对 `{component}` 组件的设计文档进行审查，生成偏差报告。

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
```

### 注意事项

- 仅审查设计文档，不涉及代码
- 以 `refactoring-plan.md` 为准
- 明确标注偏差的严重程度

```

---

## 提示词 2：设计文档修订 (Design Update)

用途：根据审查报告，修订或新建组件设计文档。

## 任务：Genesis 组件设计文档修订

### 输入
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

---

## 提示词 3：代码审查 (Code Review)

用途：对比设计文档与实际代码，生成代码偏差报告。

```markdown
## 任务：Genesis 组件代码审查

### 输入
请阅读以下内容：
1. **设计文档**：
   - `docs/{component}-design.md` — 组件设计文档（已对齐规范）

2. **规范文档**：
   - `docs/specs/component-spec.md` — 组件开发规范
   - `docs/refactoring-plan.md` — 重构计划

3. **当前代码**：
   - `pkg/{component}/` 目录下所有文件
   - `internal/{component}/` 目录下所有文件（如存在）

### 任务
对 `{component}` 组件的代码进行审查，对比设计文档，生成代码偏差报告。

### 输出格式

```markdown
# {Component} 代码审查报告

## 1. 文件结构对比
| 设计要求 | 当前文件 | 状态 | 操作建议 |
|---------|---------|------|---------|
| pkg/{component}/{component}.go | | | |
| pkg/{component}/options.go | | | |
| pkg/{component}/types/ 应移除 | | | |
| internal/{component}/ | | | |

## 2. 接口实现对比
| 设计定义 | 代码实现 | 是否一致 | 修改建议 |
|---------|---------|---------|---------|
| Interface 方法签名 | | | |
| Config 字段 | | | |
| New() 函数签名 | | | |

## 3. Option 实现检查
| Option | 是否存在 | 是否正确派生 Namespace | 修改建议 |
|--------|---------|----------------------|---------|
| WithLogger | | | |
| WithMeter | | | |
| WithTracer | | | |

## 4. 生命周期检查（如适用）
- [ ] 实现 Start(ctx) error
- [ ] 实现 Stop(ctx) error
- [ ] 资源正确释放

## 5. 导入路径检查
- [ ] 无循环依赖
- [ ] 无对 internal/ 的外部依赖（L2/L3）
- [ ] types/ 子包引用需迁移

## 6. 需要执行的重构操作清单
按优先级排序：
1. [ ] 操作描述（文件路径）
2. [ ] ...

## 7. 破坏性变更警告
- 列出可能影响外部使用者的 API 变更
```

### 注意事项

- 重点检查 types/ 子包是否需要扁平化
- 检查 WithLogger 是否正确调用 WithNamespace
- 标注破坏性变更（API 签名改变）

```

---

## 提示词 4：代码重构执行 (Code Refactor)

用途：根据代码审查报告，执行实际的代码重构。

```markdown
## 任务：Genesis 组件代码重构

### 输入
请阅读以下内容：
1. **设计文档**：
   - `docs/{component}-design.md` — 目标设计（source-of-truth）

2. **代码审查报告**：
   - 上一步生成的代码审查报告

3. **当前代码**：
   - `pkg/{component}/` 和 `internal/{component}/`

### 任务
按照代码审查报告中的操作清单，执行 `{component}` 组件的代码重构。

### 执行步骤

#### 步骤 1：types/ 扁平化（如适用于 L2/L3）
```

1. 将 pkg/{component}/types/interface.go 内容移至 pkg/{component}/{component}.go
2. 将 pkg/{component}/types/config.go 内容移至 pkg/{component}/{component}.go
3. 将 pkg/{component}/types/errors.go 内容移至 pkg/{component}/errors.go
4. 删除 pkg/{component}/types/ 目录
5. 更新所有 import 路径

```

#### 步骤 2：更新 New() 签名
```

1. 确保 New() 只接受核心依赖作为必选参数
2. Logger/Meter/Tracer 通过 Option 注入
3. 如需 Config，使用 WithConfig(cfg) 或作为必选参数

```

#### 步骤 3：修正 Option 实现
```go
// WithLogger 必须派生 namespace
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        if l != nil {
            o.logger = l.WithNamespace("{component}")
        }
    }
}
```

#### 步骤 4：更新外部引用

```
1. 搜索所有 import "genesis/pkg/{component}/types"
2. 替换为 import "genesis/pkg/{component}"
3. 更新类型引用：types.Config → {component}.Config
```

#### 步骤 5：验证

```
1. 运行 go build ./...
2. 运行 go test ./pkg/{component}/...
3. 运行 make lint
```

### 输出要求

1. 列出所有修改的文件
2. 对于每个文件，说明具体变更
3. 提供变更后的关键代码片段
4. 报告编译/测试结果

### 注意事项

- 保持向后兼容性（如需要，提供类型别名过渡期）
- 每个步骤完成后验证编译
- 使用 git 提交有意义的 commit message

```

---

## 快速参考：单组件完整流程

```bash
# 以 clog 为例的完整流程

# Step 1: 设计审查
# 使用提示词 1，输入：component=clog

# Step 2: 设计修订
# 使用提示词 2，根据审查报告修订 docs/clog-design.md

# Step 3: 代码审查
# 使用提示词 3，对比设计与代码

# Step 4: 代码重构
# 使用提示词 4，执行重构操作

# Step 5: 验证
make test
make lint
```

---

## 组件重构顺序（参考）

```
1. clog        ← L0，无依赖
2. telemetry   ← L0，无依赖
3. config      ← Glue，无 Genesis 依赖
4. connector   ← L1，保留 internal/
5. db + mq     ← L1，可并行
6. cache + idgen + dlock + idempotency  ← L2，可并行
7. ratelimit + breaker + registry       ← L3，可并行
8. container   ← Glue，最后整合
```
