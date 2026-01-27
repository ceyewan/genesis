package clog

import "context"

// noopLogger 是一个什么都不做的 Logger 实现（内部使用）
type noopLogger struct{}

// Discard 创建一个静默的 Logger 实例
//
// 返回的 Logger 实现了 Logger 接口，但所有方法体都是空操作。
func Discard() Logger {
	return &noopLogger{}
}

// 空实现 - 所有级别日志都不做任何事
func (l *noopLogger) Debug(msg string, fields ...Field)                             {}
func (l *noopLogger) Info(msg string, fields ...Field)                              {}
func (l *noopLogger) Warn(msg string, fields ...Field)                              {}
func (l *noopLogger) Error(msg string, fields ...Field)                             {}
func (l *noopLogger) Fatal(msg string, fields ...Field)                             {}
func (l *noopLogger) DebugContext(ctx context.Context, msg string, fields ...Field) {}
func (l *noopLogger) InfoContext(ctx context.Context, msg string, fields ...Field)  {}
func (l *noopLogger) WarnContext(ctx context.Context, msg string, fields ...Field)  {}
func (l *noopLogger) ErrorContext(ctx context.Context, msg string, fields ...Field) {}
func (l *noopLogger) FatalContext(ctx context.Context, msg string, fields ...Field) {}

// With 返回自身（noopLogger 的 With 方法也返回 noopLogger）
func (l *noopLogger) With(fields ...Field) Logger {
	return l
}

// WithNamespace 返回自身（noopLogger 的 WithNamespace 方法也返回 noopLogger）
func (l *noopLogger) WithNamespace(parts ...string) Logger {
	return l
}

// SetLevel 是空操作（noopLogger 不需要处理级别）
func (l *noopLogger) SetLevel(level Level) error {
	return nil
}

// Flush 是空操作（noopLogger 没有缓冲区）
func (l *noopLogger) Flush() {}
