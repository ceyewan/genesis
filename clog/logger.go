// Package clog 为 Genesis 框架提供基于 slog 的结构化日志组件。
// 支持 Context 字段提取和命名空间管理。
//
// 特性：
//   - 抽象接口，不暴露底层实现（slog）
//   - 支持层级命名空间，对于子模块 order，可使用 logger.WithNamespace("order")
//   - 零外部依赖（仅依赖 Go 标准库）
//   - 采用函数式选项模式，符合 Genesis 标准
//   - 零内存分配（Zero Allocation）设计，Field 直接映射到 slog.Attr
//   - 支持多种错误字段：Error、ErrorWithCode、ErrorWithStack
//
// 基本使用：
//
//	logger, _ := clog.New(&clog.Config{
//	    Level:  "info",
//	    Format: "console",
//	    Output: "stdout",
//	})
//	logger.Info("Hello, World!", clog.String("key", "value"))
//
// 使用函数式选项：
//
//	logger, _ := clog.New(&clog.Config{Level: "info"},
//	    clog.WithNamespace("my-service", "api"),
//	    clog.WithStandardContext(), // 自动提取 trace_id, user_id, request_id
//	)
//
// 带 Context 的日志：
//
//	ctx := context.WithValue(context.Background(), "trace-id", "abc123")
//	logger.InfoContext(ctx, "Request processed")
package clog

import "context"

// Logger 日志接口，提供结构化日志记录功能
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
