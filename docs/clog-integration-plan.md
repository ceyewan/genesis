# clog 日志库整合到 Genesis 项目计划

## 概述

本计划详细说明如何将 clog 日志库整合到 Genesis 项目中，结合 clog 的功能优势和 Genesis 的设计规范，创建一个统一、高性能的日志解决方案。

## 设计目标

### 核心原则
1. **抽象优先**：业务代码只依赖 `pkg/log` 接口
2. **功能完整**：保留 clog 的所有核心功能
3. **性能优先**：基于 zap 的零分配高性能日志
4. **可观测性**：完整的上下文感知和链路追踪

## 当前状态分析

### Genesis 当前日志接口
```go
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    WithFields(fields ...Field) Logger
    WithContext(ctx context.Context) Logger
    Sync() error
}
```

### clog 提供的功能
- 层次化命名空间系统
- 自动 TraceID 提取
- 日志轮转和文件管理
- 高性能的零分配日志记录
- 丰富的配置选项

### docs/log.md 要求的功能
- Context 感知的日志方法（`*Context`）
- 动态级别调整（`SetLevel`）
- 日志持久化（`Flush`）
- 错误增强处理

## 整合方案

### 1. 扩展 pkg/log 接口

扩展现有的 Logger 接口，添加 docs/log.md 中要求的方法：

```go
type Logger interface {
    // 基础日志方法
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)

    // Context 感知的日志方法
    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)

    // 字段和上下文增强
    With(fields ...Field) Logger
    WithContext(ctx context.Context) Logger

    // 动态配置
    SetLevel(level string) error

    // 生命周期管理
    Sync() error
    Flush() error
}
```

### 2. 实现 clog 适配器

创建 `internal/log/clog` 包，实现 pkg/log.Logger 接口：

```go
package clog

import (
    "context"
    "github.com/ceyewan/genesis/pkg/log"
    clog "github.com/ceyewan/infra-kit/clog"
)

type clogLogger struct {
    logger clog.Logger
}

func New(config *clog.Config) (log.Logger, error) {
    // 创建 clog 实例并包装为 genesis Logger
}

// 实现所有接口方法...
```

### 3. 配置系统整合

将 clog 的配置系统整合到 Genesis 的配置体系中：

```go
type Config struct {
    Level       string          `json:"level" yaml:"level"`
    Format      string          `json:"format" yaml:"format"`
    Output      string          `json:"output" yaml:"output"`
    AddSource   bool            `json:"addSource" yaml:"addSource"`
    EnableColor bool            `json:"enableColor" yaml:"enableColor"`
    RootPath    string          `json:"rootPath,omitempty" yaml:"rootPath,omitempty"`
    Rotation    *RotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
```

### 4. 上下文集成

实现 docs/log.md 中要求的 Context 感知功能：

```go
// 自动从 context 提取 TraceID、RequestID、UserID
func (l *clogLogger) DebugContext(ctx context.Context, msg string, fields ...log.Field) {
    // 自动注入上下文字段
    allFields := append(l.extractContextFields(ctx), fields...)
    l.logger.Debug(msg, allFields...)
}
```

### 5. 错误增强处理

实现错误字段的自动拆解：

```go
func ErrorField(err error) log.Field {
    return func(b *LogBuilder) {
        if err == nil {
            return
        }
        b.data["err_msg"] = err.Error()
        b.data["err_stack"] = extractStack(err)
    }
}
```

## 具体实施步骤

### 阶段一：接口扩展
1. 扩展 `pkg/log/log.go` 接口
2. 添加 `*Context` 方法
3. 添加 `SetLevel` 和 `Flush` 方法

### 阶段二：clog 适配器实现
1. 创建 `internal/log/clog` 包
2. 实现 pkg/log.Logger 接口
3. 整合 clog 的层次化命名空间
4. 实现 TraceID 自动提取

### 阶段三：配置整合
1. 创建统一的配置结构
2. 实现环境相关的默认配置
3. 支持依赖注入（fx）

### 阶段四：功能增强
1. 实现错误增强处理
2. 添加日志轮转支持
3. 完善测试用例

### 阶段五：文档和示例
1. 更新 README.md
2. 创建使用示例
3. 编写迁移指南

## 技术决策

### 关于 slog 迁移
**不推荐迁移到 slog**，原因：
- clog 基于 zap 的性能优势明显
- clog 已经实现了 docs/log.md 中要求的大部分功能
- 迁移到 slog 需要重写大量代码，收益有限
- clog 的层次化命名空间和自动 TraceID 提取是独特优势

### 性能考虑
- 保持 clog 的零分配设计
- 优化上下文字段提取性能
- 避免反射开销

### 兼容性
- 保持向后兼容性
- 提供迁移路径
- 渐进式采用

## 文件结构变更

```
genesis/
├── pkg/
│   └── log/
│       ├── log.go              # 扩展后的接口
│       └── fields.go           # 字段定义
├── internal/
│   └── log/
│       ├── zap/                # 现有的 zap 实现（可选保留）
│       └── clog/              # 新的 clog 适配器
│           ├── adapter.go     # 接口适配器
│           ├── config.go       # 配置处理
│           └── fields.go      # 字段处理
└── examples/
    └── logging/
        ├── main.go            # 更新示例
        └── advanced.go        # 高级用法示例
```

## 迁移策略

### 渐进式迁移
1. 新代码使用新的 Logger 接口
2. 现有代码可以继续使用旧接口
3. 提供适配器包装旧代码

### 向后兼容
- 保持现有 API 不变
- 新增功能通过新方法提供
- 提供配置选项启用新功能

## 测试计划

### 单元测试
- 接口契约测试
- 上下文字段提取测试
- 错误增强处理测试

### 集成测试
- 配置加载测试
- 日志轮转测试
- 性能基准测试

### 兼容性测试
- 现有代码兼容性
- 配置迁移测试

## 风险评估

### 技术风险
- **低**：clog 已经经过生产验证
- **中**：接口变更可能影响现有代码

### 缓解措施
- 充分的测试覆盖
- 渐进式迁移策略
- 详细的迁移文档

## 时间估算

| 阶段 | 时间估算 | 优先级 |
|------|----------|--------|
| 接口扩展 | 1天 | 高 |
| clog 适配器 | 2天 | 高 |
| 配置整合 | 1天 | 中 |
| 功能增强 | 2天 | 中 |
| 文档和示例 | 1天 | 低 |
| 测试 | 1天 | 高 |
| **总计** | **8天** | |

## 下一步行动

1. **切换到 Code 模式**执行具体代码修改
2. 按照本计划分阶段实施
3. 每个阶段完成后进行测试
4. 更新文档和示例

## 结论

clog 提供了强大的日志功能，可以很好地整合到 Genesis 项目中。通过创建适配器层，我们可以在保持 Genesis 设计原则的同时，充分利用 clog 的高级功能。不建议迁移到 slog，因为 clog 已经满足了所有需求且性能更优。