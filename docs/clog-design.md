# clog 设计文档

## 1. 概述

`clog` 是 Genesis 框架的结构化日志组件，基于 Go 标准库 `log/slog` 构建。

* **所属层级**：L0 (Base) — 框架基石，被所有上层组件依赖
* **核心职责**：提供统一的结构化日志接口，支持命名空间派生和 Context 字段提取
* **设计原则**：
  * 抽象接口，不暴露底层实现（slog）
  * 支持层级命名空间，适配微服务架构
  * 零外部依赖（仅依赖 Go 标准库）

## 2. 目录结构

### 2.1 当前结构

```text
pkg/clog/
├── clog.go              # 工厂函数 (New, Default, Must) + 类型重导出
└── types/               # 类型定义（待扁平化）
    ├── config.go        # Config 结构体
    ├── fields.go        # Field 构造函数
    ├── level.go         # Level 定义
    └── logger.go        # Logger 接口

internal/clog/
├── logger.go            # Logger 接口实现
├── context.go           # Context 字段提取
├── namespace.go         # 命名空间处理
└── slog/
    └── handler.go       # slog Handler 适配器
```

### 2.2 目标结构（扁平化后）

```text
pkg/clog/
├── clog.go              # 工厂函数 + Logger 接口 + Config + Level
├── fields.go            # Field 类型与构造函数
├── options.go           # Option 模式定义
└── errors.go            # Sentinel Errors（如需要）

internal/clog/
├── logger.go            # 实现细节
├── context.go
├── namespace.go
└── slog/
    └── handler.go
```

**说明**：作为 L0 组件，`clog` 保留 `internal/` 实现以隔离 slog 适配逻辑；但 `pkg/clog/types/` 应扁平化到 `pkg/clog/` 根目录。

## 3. 接口定义

### 3.1 Logger 接口

```go
// Logger 定义了所有日志操作
type Logger interface {
    // 基础日志级别方法
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    // 带 Context 的日志方法，自动提取 Context 中的字段
    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)
    FatalContext(ctx context.Context, msg string, fields ...Field)

    // With 创建带预设字段的子 Logger
    With(fields ...Field) Logger

    // WithNamespace 创建扩展命名空间的子 Logger
    WithNamespace(parts ...string) Logger

    // SetLevel 动态调整日志级别
    SetLevel(level Level) error

    // Flush 强制同步缓冲区
    Flush()
}
```

### 3.2 Config 结构体

```go
// Config 日志配置
type Config struct {
    Level       string `json:"level" yaml:"level"`           // debug|info|warn|error|fatal
    Format      string `json:"format" yaml:"format"`         // json|console
    Output      string `json:"output" yaml:"output"`         // stdout|stderr|<file path>
    EnableColor bool   `json:"enableColor" yaml:"enableColor"`
    AddSource   bool   `json:"addSource" yaml:"addSource"`
    SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"` // 用于裁剪文件路径前缀
}
```

### 3.3 Level 定义

```go
type Level int

const (
    DebugLevel Level = iota
    InfoLevel
    WarnLevel
    ErrorLevel
    FatalLevel
)
```

### 3.4 Field 类型

```go
// Field 是构建日志字段的抽象类型
type Field func(*LogBuilder)
```

## 4. 工厂函数

### 4.1 New

```go
// New 创建 Logger 实例
// config: 日志配置（必选）
// option: 实例选项（可选，传 nil 使用默认值）
func New(config *Config, option *Option) (Logger, error)
```

### 4.2 Default

```go
// Default 创建默认配置的 Logger（info 级别，console 格式，stdout 输出）
func Default() Logger
```

### 4.3 Must

```go
// Must 类似 New，但出错时 panic（仅用于初始化）
func Must(config *Config, option *Option) Logger
```

## 5. Option 模式

### 5.1 Option 结构体

```go
type Option struct {
    NamespaceParts  []string        // 初始命名空间，如 ["order-service"]
    ContextFields   []ContextField  // Context 字段提取规则
    ContextPrefix   string          // Context 字段前缀，默认 "ctx."
    NamespaceJoiner string          // 命名空间连接符，默认 "."
}

type ContextField struct {
    Key       any                    // Context 中的键
    FieldName string                 // 输出字段名
    Required  bool                   // 是否必须
    Extract   func(any) (any, bool)  // 自定义提取函数
}
```

### 5.2 作为其他组件的 Option

其他 Genesis 组件通过 `WithLogger` 注入 clog：

```go
// 示例：dlock 组件的 options.go
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        if l != nil {
            o.logger = l.WithNamespace("dlock")  // 自动派生命名空间
        }
    }
}
```

## 6. 命名空间规范

### 6.1 层级结构

| 层级 | 格式 | 示例 |
|------|------|------|
| 应用级 | `<app>` | `user-service` |
| 组件级 | `<app>.<component>` | `user-service.dlock` |
| 子模块级 | `<app>.<component>.<sub>` | `user-service.dlock.redis` |

### 6.2 使用示例

```go
// main.go - 创建应用级 Logger
appLogger := clog.Must(&clog.Config{
    Level:  "info",
    Format: "json",
    Output: "stdout",
}, nil).WithNamespace("user-service")

// 传入组件 - 组件内部会追加自己的命名空间
locker, _ := dlock.New(redisConn,
    dlock.WithLogger(appLogger),  // 内部变为 "user-service.dlock"
)

// 组件内部继续派生
redisLogger := logger.WithNamespace("redis")  // "user-service.dlock.redis"
```

## 7. Field 构造函数

### 7.1 基础类型

```go
func String(key, value string) Field
func Int(key string, value int) Field
func Int64(key string, value int64) Field
func Float64(key string, value float64) Field
func Bool(key string, value bool) Field
func Duration(key string, value time.Duration) Field
func Time(key string, value time.Time) Field
func Any(key string, value any) Field
```

### 7.2 错误处理

```go
// Error 将错误拆解为结构化字段
// 输出: err_msg, err_type, err_stack
func Error(err error) Field

// ErrorWithCode 附加业务错误码
// 额外输出: err_code
func ErrorWithCode(err error, code string) Field
```

| 字段名 | 描述 |
|--------|------|
| `err_msg` | 错误消息 (`err.Error()`) |
| `err_type` | 错误类型 |
| `err_stack` | 错误堆栈 |
| `err_code` | 业务错误码（可选） |

### 7.3 语义字段

```go
func RequestID(id string) Field   // → request_id
func UserID(id string) Field      // → user_id
func TraceID(id string) Field     // → trace_id
func Component(name string) Field // → component
```

## 8. 使用示例

### 8.1 基础用法

```go
logger := clog.Default()

logger.Info("service started",
    clog.String("version", "v1.0.0"),
    clog.Int("port", 8080))

logger.Error("operation failed",
    clog.Error(err),
    clog.String("operation", "createUser"))
```

### 8.2 Context 字段提取

```go
// 假设 ctx 中包含 request_id
logger.InfoContext(ctx, "user login",
    clog.String("method", "password"))
// 输出: {"ctx.request_id":"req-123", "method":"password", ...}
```

### 8.3 生产环境配置

```go
logger := clog.Must(&clog.Config{
    Level:      "warn",
    Format:     "json",
    Output:     "stdout",
    AddSource:  true,
    SourceRoot: "genesis",  // 裁剪路径前缀
}, nil)
```

## 9. 技术实现

* **底层实现**：基于 Go 1.21+ 标准库 `log/slog`
* **性能优化**：利用 slog 的延迟计算特性，复用 LogBuilder 减少内存分配
* **扩展性**：通过 `internal/clog/slog/handler.go` 封装，便于未来替换底层实现

## 10. 重构计划

### 10.1 待完成项

* [ ] **扁平化 types/**：将 `pkg/clog/types/` 内容移至 `pkg/clog/` 根目录
* [ ] **控制台颜色**：实现 `console` 格式的颜色输出
* [ ] **Caller 定位优化**：修复 `runtime.Caller` 魔法数字问题
* [ ] **Context 提取性能**：减少反射开销

### 10.2 不变项

* Logger 接口签名保持稳定
* Field 构造函数签名保持稳定
* Config 结构体字段保持兼容
