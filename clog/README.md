[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/clog.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/clog)

# clog - Genesis 结构化日志组件

`clog` 是 Genesis 框架的结构化日志组件，基于 Go 标准库 `log/slog` 构建。

## 特性

- **所属层级**：L0 (Base) — 框架基石，被所有上层组件依赖
- **核心职责**：提供统一的结构化日志接口，支持命名空间派生和 Context 字段提取
- **设计原则**：
    - 抽象接口，不暴露底层实现（slog）
    - 支持层级命名空间，适配微服务架构
    - 零外部依赖（仅依赖 Go 标准库）
    - 采用函数式选项模式，符合 Genesis 标准

## 2. 目录结构

```text
clog/                     # 根目录 - 扁平化设计
├── README.md             # 本文档
├── clog.go              # 构造函数：New()
├── config.go            # 配置结构：Config + validate()
├── options.go           # 函数式选项：Option、WithNamespace 等
├── logger.go            # Logger 接口定义
├── level.go             # 日志级别：Level、ParseLevel
├── fields.go            # 字段构造函数：String、Error 等
├── impl.go              # Logger 实现（私有）
├── context.go           # Context 字段提取（私有）
├── namespace.go         # 命名空间处理（私有）
└── slog_handler.go      # slog Handler 适配器（私有）
```

**说明**：采用完全扁平化设计，所有公开 API 直接在根目录，实现细节通过私有函数隐藏。

## 3. 核心接口

### 3.1 Logger 接口

```go
// Logger 日志接口，提供结构化日志记录功能
//
// 支持五个日志级别：Debug、Info、Warn、Error、Fatal
// 每个级别都有带 Context 和不带 Context 的版本
//
// 基本使用：
//   logger.Info("Hello, World", clog.String("key", "value"))
//
// 带 Context 的使用：
//   logger.InfoContext(ctx, "Request processed")
//   // 会自动从 Context 中提取配置的字段
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

### 3.2 Config 结构体

```go
// Config 日志配置结构，定义日志的基本行为
//
// 支持的配置项：
//   Level: 日志级别 (debug|info|warn|error|fatal)
//   Format: 输出格式 (json|console)
//   Output: 输出目标 (stdout|stderr|文件路径)
//   EnableColor: 是否启用彩色输出（仅 console 格式）
//   AddSource: 是否显示调用位置信息
//   SourceRoot: 源代码路径前缀，用于裁剪显示的文件路径
type Config struct {
    Level       string `json:"level" yaml:"level"`             // debug|info|warn|error|fatal
    Format      string `json:"format" yaml:"format"`           // json|console
    Output      string `json:"output" yaml:"output"`           // stdout|stderr|<file path>
    EnableColor bool   `json:"enableColor" yaml:"enableColor"` // 仅在 console 格式下有效，未实现
    AddSource   bool   `json:"addSource" yaml:"addSource"`
    SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"` // 用于裁剪文件路径
}
```

### 3.3 函数式选项模式

```go
// Option 函数式选项，用于配置 Logger 实例
type Option func(*options)

// WithNamespace 设置日志命名空间，支持多级命名空间
func WithNamespace(parts ...string) Option

// WithStandardContext 自动提取标准的上下文字段
func WithStandardContext() Option

// WithContextField 添加自定义的 Context 字段提取规则
func WithContextField(key any, fieldName string, opts ...ContextFieldOption) Option
```

## 4. 工厂函数

```go
// New 创建一个新的 Logger 实例
//
// config 为日志基本配置，opts 为函数式选项。
func New(config *Config, opts ...Option) (Logger, error)
```

## 5. 命名空间规范

### 5.1 层级结构

| 层级     | 格式                      | 示例                       |
| -------- | ------------------------- | -------------------------- |
| 应用级   | `<app>`                   | `user-service`             |
| 组件级   | `<app>.<component>`       | `user-service.dlock`       |
| 子模块级 | `<app>.<component>.<sub>` | `user-service.dlock.redis` |

### 5.2 使用示例

```go
// main.go - 创建应用级 Logger
logger, _ := clog.New(&clog.Config{
    Level:  "info",
    Format: "json",
    Output: "stdout",
}, clog.WithNamespace("user-service"))

// 组件内部派生
subLogger := logger.WithNamespace("api")
// 最终命名空间: "user-service.api"
```

## 6. Field 构造函数

### 6.1 基础类型

```go
func String(k, v string) Field    // → k="value"
func Int(k string, v int) Field     // → k=123
func Float64(k string, v float64) Field  // → k=123.45
func Bool(k string, v bool) Field     // → k=true
func Time(k string, v time.Time) Field   // → k="2023-01-01T00:00:00Z"
func Any(k string, v any) Field       // → k=<任意值>
```

### 6.2 错误处理

```go
// Error 将错误结构化为标准字段
// 输出: err_msg, err_type, err_stack
func Error(err error) Field

// ErrorWithCode 包含错误代码和堆栈信息的错误字段
// 额外输出: err_code
func ErrorWithCode(err error, code string) Field
```

### 6.3 Context 字段提取

```go
// WithContextField 添加自定义 Context 字段提取
clog.WithContextField("trace-id", "trace_id", clog.Required(true))

// WithStandardContext 自动提取标准字段
clog.WithStandardContext()  // trace_id, user_id, request_id
```

## 7. 使用示例

### 7.1 基础用法

```go
logger, _ := clog.New(&clog.Config{
    Level:  "info",
    Format: "console",
    Output: "stdout",
})

logger.Info("service started",
    clog.String("version", "v1.0.0"),
    clog.Int("port", 8080))

logger.Error("operation failed",
    clog.Error(err),
    clog.String("operation", "createUser"))
```

### 7.2 使用函数式选项

```go
logger, _ := clog.New(&clog.Config{
    Level:  "info",
    Format: "json",
    Output: "stdout",
},
    clog.WithNamespace("order-service", "api"),
    clog.WithStandardContext(),
)
```

### 7.3 Context 字段提取

```go
// 设置 Context
ctx := context.WithValue(context.Background(), "trace_id", "abc123")
ctx = context.WithValue(ctx, "user_id", "user-456")

// 使用 WithStandardContext 自动提取
logger, _ := clog.New(&clog.Config{Level: "info"}, clog.WithStandardContext())
logger.InfoContext(ctx, "Request processed")
// 输出: {"ctx.trace_id":"abc123", "ctx.user_id":"user-456", "msg":"Request processed", ...}
```

### 7.4 生产环境配置

```go
logger, _ := clog.New(&clog.Config{
    Level:      "warn",
    Format:     "json",
    Output:     "/var/log/app.log",
    AddSource:  true,
    SourceRoot: "/app",
})
```

## 8. 技术实现

- **底层实现**：基于 Go 1.21+ 标准库 `log/slog`
- **性能优化**：利用 slog 的延迟计算特性，直接使用 map[string]any 减少内存分配
- **扩展性**：通过私有函数封装，便于未来替换底层实现
- **兼容性**：提供完整的 API 文档和使用示例

## 9. API 参考

完整的 API 文档可以通过以下方式查看：

```bash
# 查看完整的包文档
go doc -all ./clog

# 查看特定函数
go doc ./clog.New
go doc ./clog.WithNamespace
go doc ./clog.String
```

## 10. 最佳实践

1. **命名空间使用**：应用级 Logger 设置主服务名，组件内使用 WithNamespace 追加
2. **Context 提取**：使用 WithStandardContext 自动提取标准字段，或自定义配置
3. **错误处理**：使用 Error/ErrorWithCode 统一错误日志格式
4. **性能考虑**：避免在热路径中创建大量 Field，复用 Logger 实例
