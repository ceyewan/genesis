# API 库

## clog

```go
package clog // import "github.com/ceyewan/genesis/clog"

Package clog 为 Genesis 框架提供基于 slog 的结构化日志组件。

特性：
  - 抽象接口，不暴露底层实现（slog）
  - 支持层级命名空间，适配微服务架构
  - 零外部依赖（仅依赖 Go 标准库）
  - 零内存分配（Zero Allocation）设计，Field 直接映射到 slog.Attr

type Config struct {
    Level       string // debug|info|warn|error|fatal
    Format      string // json|console
    Output      string // stdout|stderr|<file path>
    EnableColor bool   // 仅 console 格式有效
    AddSource   bool   // 是否显示调用位置
    SourceRoot  string // 用于裁剪文件路径
}
    func NewDevDefaultConfig(sourceRoot string) *Config
    func NewProdDefaultConfig(sourceRoot string) *Config

type Logger interface {
    // 基础日志方法
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    // 带 Context 的日志方法，自动提取配置的字段
    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)
    FatalContext(ctx context.Context, msg string, fields ...Field)

    // 子 Logger：添加预设字段
    With(fields ...Field) Logger

    // 子 Logger：扩展命名空间
    WithNamespace(parts ...string) Logger

    // 动态调整日志级别
    SetLevel(level Level) error

    // 强制同步缓冲区
    Flush()
}

type Level int
const (
    DebugLevel Level = iota - 4; InfoLevel; WarnLevel; ErrorLevel; FatalLevel
)
    func ParseLevel(s string) (Level, error)

type Field = slog.Attr
    func Any(k string, v any) Field
    func Bool(k string, v bool) Field
    func Duration(k string, v time.Duration) Field
    func Error(err error) Field           // 轻量级，仅 err_msg
    func ErrorWithCode(err error, code string) Field
    func ErrorWithCodeStack(err error, code string) Field
    func ErrorWithStack(err error) Field // 包含堆栈信息，无Code
    func Float64(k string, v float64) Field
    func Int(k string, v int) Field
    func Int64(k string, v int64) Field
    func String(k, v string) Field
    func Time(k string, v time.Time) Field

type Option func(*options)
    func WithContextField(key any, fieldName string) Option
    func WithNamespace(parts ...string) Option
    func WithStandardContext() Option  // trace_id, user_id, request_id

func Discard() Logger  // No-op Logger
func New(config *Config, opts ...Option) (Logger, error)
```
