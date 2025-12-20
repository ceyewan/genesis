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

```text
pkg/clog/
├── clog.go              # 工厂函数 + Logger 接口 + Config + Level
├── fields.go            # Field 类型与构造函数
├── options.go           # Option 模式定义
└── errors.go            # Sentinel Errors（如需要）

internal/clog/
├── logger.go            # 实现细节
├── context.go           # Context 字段提取
├── namespace.go         # 命名空间处理
└── slog/
    └── handler.go       # slog Handler 适配器
```

**说明**：作为 L0 组件，`clog` 保留 `internal/` 实现以隔离 slog 适配逻辑。

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

```go
// New 创建 Logger 实例
func New(config *Config, opts ...Option) (Logger, error)

// Default 创建默认配置的 Logger（info 级别，console 格式，stdout 输出）
func Default() Logger

// Must 类似 New，但出错时 panic（仅用于初始化）
func Must(config *Config, opts ...Option) Logger
```

## 5. Option 模式

```go
type options struct {
    namespaceParts  []string        // 初始命名空间，如 ["order-service"]
    contextFields   []ContextField  // Context 字段提取规则
    contextPrefix   string          // Context 字段前缀，默认 "ctx."
    namespaceJoiner string          // 命名空间连接符，默认 "."
}

type Option func(*options)

func WithNamespace(parts ...string) Option
func WithContextFields(fields ...ContextField) Option
func WithContextPrefix(prefix string) Option
func WithNamespaceJoiner(joiner string) Option
```

## 6. 命名空间规范

### 6.1 层级结构

| 层级 | 格式 | 示例 |
| ------ | ------ | ------ |
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
}).WithNamespace("user-service")

// 传入组件 - 组件内部会追加自己的命名空间
locker, _ := dlock.NewRedis(redisConn, &dlock.Config{},
    dlock.WithLogger(appLogger),  // 内部变为 "user-service.dlock"
)

// Connector 内部派生
// logger.WithNamespace("redis")  → "user-service.connector.redis"
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
// 输出: err_msg, err_type
func Error(err error) Field

// ErrorWithCode 附加业务错误码
// 额外输出: err_code
func ErrorWithCode(err error, code string) Field
```

### 7.3 语义字段

```go
func RequestID(id string) Field   // → request_id
func UserID(id string) Field      // → user_id
func Component(name string) Field // → component
```

## 8. 与其他组件的集成

### 8.1 Connector 集成

所有 Connector 通过 `WithLogger` 注入 Logger：

```go
// pkg/connector/options.go
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("connector")
    }
}

// 在 Connector 内部使用
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
    // ...
    c.logger = opt.logger.WithNamespace("redis")
    // 输出: {"namespace":"user-service.connector.redis", ...}
}
```

### 8.2 组件集成

所有 L2/L3 组件遵循相同模式：

```go
// pkg/dlock/options.go
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("dlock")
    }
}

// pkg/cache/options.go
func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("cache")
    }
}
```

### 8.3 典型使用流程

```go
func main() {
    // 1. 创建应用级 Logger
    logger := clog.Must(&clog.Config{
        Level:  "info",
        Format: "json",
    }).WithNamespace("order-service")

    // 2. 注入到 Connector
    redisConn, _ := connector.NewRedis(&cfg.Redis,
        connector.WithLogger(logger),
    )
    // 日志命名空间: order-service.connector.redis

    // 3. 注入到组件
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock,
        dlock.WithLogger(logger),
    )
    // 日志命名空间: order-service.dlock
}
```

## 9. 使用示例

### 9.1 基础用法

```go
logger := clog.Default()

logger.Info("service started",
    clog.String("version", "v1.0.0"),
    clog.Int("port", 8080))

logger.Error("operation failed",
    clog.Error(err),
    clog.String("operation", "createUser"))
```

### 9.2 Context 字段提取

```go
// 假设 ctx 中包含 request_id
logger.InfoContext(ctx, "user login",
    clog.String("method", "password"))
// 输出: {"ctx.request_id":"req-123", "method":"password", ...}
```

### 9.3 生产环境配置

```go
logger := clog.Must(&clog.Config{
    Level:      "warn",
    Format:     "json",
    Output:     "stdout",
    AddSource:  true,
    SourceRoot: "genesis",  // 裁剪路径前缀
})
```

## 10. 技术实现

* **底层实现**：基于 Go 1.21+ 标准库 `log/slog`
* **性能优化**：利用 slog 的延迟计算特性，复用 LogBuilder 减少内存分配
* **扩展性**：通过 `internal/clog/slog/handler.go` 封装，便于未来替换底层实现
