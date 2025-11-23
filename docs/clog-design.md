# clog 日志库设计文档

## 1. 目标与原则

`clog` 旨在提供一个基于 `slog` 的、简洁、易用且高度抽象的日志库，用于替代现有直接暴露底层实现的日志方案。

**设计原则：**

1. **抽象接口：** 永不暴露底层日志库的类型。
2. **简洁 API：** 统一使用 `Config + Option` 进行配置。
3. **标准化：** 统一错误字段、Context 字段和命名空间结构。
4. **去冗余：** 默认不内置文件轮转逻辑，鼓励使用外部收集器。
5. **层级命名空间：** 支持递归扩展命名空间，便于微服务架构中的组件标识。

## 2. 项目结构

遵循 Go 标准项目布局，核心实现与 API 分离：

```text
genesis/
├── pkg/
│   └── clog/                    # 公开API入口
│       └── types/              # 类型定义（接口、结构体、字段）
├── internal/
│   └── clog/                   # 内部实现
│       ├── logger.go           # Logger接口实现
│       ├── context.go          # Context字段提取
│       ├── namespace.go        # 命名空间处理
│       └── slog/               # slog适配器
│           └── handler.go      
├── examples/
└── ...
```

依赖关系图：

```text
用户代码
    ↓ import
pkg/clog (工厂函数 + 重新导出)
    ↓ import
pkg/clog/types (接口定义)
    ↑ implement
internal/clog (具体实现)
    ↓ import  
internal/clog/slog (slog适配器)
```

## 3. 核心 API 设计

核心 API 位于 `pkg/clog/types/` 包中，通过 `Field` 抽象和 `Logger` 接口实现。

### 3.1. 字段抽象 (Field)

使用 `Field` 抽象函数来构建日志字段，避免直接暴露底层类型。

```go
// Field 是用于构建日志字段的抽象类型。
type Field func(*LogBuilder)
```

### 3.2. Logger 接口

`Logger` 接口定义了所有日志操作，支持基础日志、带 Context 的日志和链式操作。

```go
type Logger interface {
    // 基础日志级别方法
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    // 带 Context 的日志级别方法，用于自动提取 Context 字段
    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)
    FatalContext(ctx context.Context, msg string, fields ...Field)

    // With 创建一个带有预设字段的子 Logger
    With(fields ...Field) Logger
    
    // WithNamespace 创建一个扩展命名空间的子 Logger
    WithNamespace(parts ...string) Logger
    
    // SetLevel 动态调整日志级别
    SetLevel(level Level) error
    
    // Flush 强制同步所有缓冲区的日志
    Flush()
}
```

## 4. 配置与选项设计

配置分为全局配置 `Config` 和实例选项 `Option`。

### 4.1. Config (全局配置)

用于控制日志的级别、格式、输出目标、是否启用颜色和源码信息。

```go
type Config struct {
    Level       string `json:"level" yaml:"level"`         // debug|info|warn|error|fatal
    Format      string `json:"format" yaml:"format"`       // json|console
    Output      string `json:"output" yaml:"output"`       // stdout|stderr|<file path>
    EnableColor bool   `json:"enableColor" yaml:"enableColor"`
    AddSource   bool   `json:"addSource" yaml:"addSource"`
    SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"` // 用于裁剪文件路径
}
```

### 4.2. Option (实例选项)

主要用于配置命名空间和 Context 字段提取规则。

```go
type ContextField struct {
    Key         any         // Context 中存储的键
    FieldName   string      // 输出的最终字段名，如 "ctx.trace_id"
    Required    bool        // 是否必须存在
    Extract     func(any) (any, bool) // 可选的自定义提取函数
}

type Option struct {
    NamespaceParts    []string        // 多级命名空间，如 ["order-service", "handler"]
    ContextFields     []ContextField  // Context 字段提取规则
    ContextPrefix     string          // Context 字段前缀，默认 "ctx."
    NamespaceJoiner   string          // 命名空间连接符，默认 "."
}
```

## 5. 字段构造函数标准化

所有字段构造函数位于 `pkg/clog/types/fields.go`。

### 5.1. 基础类型

提供所有基础类型的构造函数，返回 `Field`。

```go
func String(k, v string) Field
func Int(k string, v int) Field
// ... 更多基础类型，如 Int64, Float64, Bool, Duration, Time, Any
```

### 5.2. 错误处理标准化

错误字段统一拆解为结构化字段，便于检索和分析。

| 字段名 | 描述 |
|---|---|
| `err_msg` | 错误消息 (Error()) |
| `err_type` | 错误类型 |
| `err_stack` | 错误堆栈（包含文件名、行号、函数名） |
| `err_code` | 可选的业务错误码 |

```go
func Error(err error) Field
func ErrorWithCode(err error, code string) Field
```

### 5.3. 常用语义字段

提供常用语义字段的别名，确保字段名统一。

```go
func RequestID(id string) Field          // 映射到 request_id
func UserID(id string) Field             // 映射到 user_id
func TraceID(id string) Field            // 映射到 trace_id
func Component(name string) Field        // 映射到 component
```

## 6. 命名空间设计

clog 支持递归扩展命名空间，适用于微服务架构。推荐的命名空间规则为：

- 应用级：`<app>`，例如 `user-service`
- 组件级：`<app>.<component>`，例如 `user-service.dlock`
- 子模块级：`<app>.<component>.<sub>`，例如 `user-service.dlock.redis`

### 6.1. 与 Container / 组件的协同

在典型的 Genesis 应用中：

```go
// main.go 中创建应用级 Logger
appLogger := clog.New(appCfg.Log).WithNamespace(appCfg.App.Namespace) // 如 "user-service"

// Container 在装配组件时，仅传入 appLogger
locker, _ := dlock.New(dep, cfg,
	dlock.WithLogger(appLogger), // dlock 内部会在 Option 中追加 "dlock" 命名空间
)

// dlock 组件内部如需更细分的子模块日志，可继续派生
redisLogger := lockerLogger.WithNamespace("redis") // 最终 namespace: "user-service.dlock.redis"
```

## 7. 如何使用 (用户指南)

### 7.1. 初始化 Logger

使用 `clog.New` 或 `clog.Default` 初始化日志实例。

```go
// 生产环境配置示例：JSON格式，WARN级别，显示相对路径
logger := clog.New(&clog.Config{
    Level:       "warn",
    Format:      "json",
    Output:      "stdout",
    AddSource:   true,
    SourceRoot:  "genesis",
}, nil)

logger.Info("service starting", 
    clog.String("version", "v1.0.0"),
    clog.Int("port", 8080))
```

### 7.2. Context 字段提取 (简洁说明)

`clog` 支持从 `context.Context` 中自动提取预配置的字段（如 `request_id`, `trace_id`），并默认添加 `ctx.` 前缀以避免冲突。

```go
// 假设 ctx 包含 request_id
logger.InfoContext(ctx, "user login successful",
    clog.String("method", "password"))
// 输出: {"ctx.request_id":"req-12345", "method":"password"}
```

### 7.3. 错误处理最佳实践

使用结构化的错误字段，包含完整的错误信息和堆栈。

```go
err := errors.New("database connection failed")
logger.Error("operation failed", clog.Error(err))
// 输出: {"err_msg":"database connection failed","err_type":"*errors.errorString","err_stack":"..."}
```

## 8. 技术实现与扩展性

- **底层库：** 基于 Go 标准库 `log/slog`。
- **性能：** 利用 `slog` 的高性能特性和延迟计算，并通过复用 `LogBuilder` 对象减少内存分配。
- **扩展性：** 通过 `internal/clog/slog/handler.go` 封装，便于未来切换底层实现。

## 9. TODO 列表

- [ ] **控制台颜色输出优化：** 确保在 `console` 格式下，日志级别和字段能够正确显示颜色，以提高开发环境的可读性。
- [ ] **Context 提取性能：** 优化 Context 字段提取逻辑，减少不必要的反射或类型断言开销。
- [ ] **文档示例完善：** 补充 `clog.Default()` 的使用示例。
