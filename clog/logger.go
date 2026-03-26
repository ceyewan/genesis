// Package clog 为 Genesis 提供统一的结构化日志接口。
// 它基于 slog，支持命名空间管理、Context 字段提取和运行时级别调整。
//
// 特性：
//   - 抽象接口，不暴露底层实现（slog）
//   - 支持层级命名空间，对于子模块 order，可使用 logger.WithNamespace("order")
//   - 基于标准库 slog，额外支持可选的 OpenTelemetry Trace 上下文字段提取
//   - 采用函数式选项模式，符合 Genesis 标准
//   - Field 直接映射到 slog.Attr，减少字段适配成本
//   - 支持统一的 error 结构化字段输出
//
// 基本使用：
//
//	logger, _ := clog.New(&clog.Config{
//	    Level:  "info",
//	    Format: "console",
//	    Output: "stdout",
//	})
//	defer logger.Close()
//	logger.Info("Hello, World!", clog.String("key", "value"))
//
// 使用函数式选项：
//
//	logger, _ := clog.New(&clog.Config{Level: "info"},
//	    clog.WithNamespace("my-service", "api"),
//	    clog.WithContextField("trace_id", "trace_id"),
//	    clog.WithContextField("user_id", "user_id"),
//	    clog.WithContextField("request_id", "request_id"),
//	)
//	defer logger.Close()
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

	// Fatal 记录 FATAL 级别日志。
	//
	// Fatal 只负责记录日志，不会退出进程，进程生命周期由调用方自行控制。
	Fatal(msg string, fields ...Field)

	// 带 Context 的日志级别方法，用于自动提取 Context 字段
	DebugContext(ctx context.Context, msg string, fields ...Field)
	InfoContext(ctx context.Context, msg string, fields ...Field)
	WarnContext(ctx context.Context, msg string, fields ...Field)
	ErrorContext(ctx context.Context, msg string, fields ...Field)

	// FatalContext 记录带 Context 的 FATAL 级别日志。
	//
	// FatalContext 只负责记录日志，不会退出进程，进程生命周期由调用方自行控制。
	FatalContext(ctx context.Context, msg string, fields ...Field)

	// With 创建一个带有预设字段的子 Logger
	With(fields ...Field) Logger

	// WithNamespace 创建一个扩展命名空间的子 Logger
	WithNamespace(parts ...string) Logger

	// SetLevel 动态调整日志级别
	SetLevel(level Level) error

	// Flush 强制同步所有缓冲区的日志
	Flush()

	// Close 释放 Logger 持有的资源。
	//
	// 当 Output 配置为文件路径时，调用方应在不再使用 Logger 后执行 Close。
	// 对 stdout、stderr 和 Discard Logger，Close 是 no-op。
	Close() error
}
