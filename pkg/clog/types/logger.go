package types

import "context"

// Logger 接口定义了所有日志操作。
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
