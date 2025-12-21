// Package clog 为 Genesis 框架提供基于 slog 的结构化日志组件。
// 支持 Context 字段提取和命名空间管理。
//
// 特性：
//   - 抽象接口，不暴露底层实现（slog）
//   - 支持层级命名空间，适配微服务架构
//   - 零外部依赖（仅依赖 Go 标准库）
//   - 采用函数式选项模式，符合 Genesis 标准
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
//
// 支持五个日志级别：Debug、Info、Warn、Error、Fatal
// 每个级别都有带 Context 和不带 Context 的版本
//
// 基本使用：
//
//	logger.Info("Hello, World", clog.String("key", "value"))
//
// 带 Context 的使用：
//
//	logger.InfoContext(ctx, "Request processed")
//	// 会自动从 Context 中提取配置的字段
//
// 创建子 Logger：
//
//	childLogger := logger.With(clog.String("module", "auth"))
//	namespacedLogger := logger.WithNamespace("auth", "login")
type Logger interface {
	// 基础日志级别方法
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)

	// 带 Context 的日志级别方法，用于自动提取 Context 字段
	//
	// 示例：
	//   ctx := context.WithValue(context.Background(), "trace_id", "abc123")
	//   logger.InfoContext(ctx, "Request processed")
	//   // 日志中会包含提取的 Context 字段
	DebugContext(ctx context.Context, msg string, fields ...Field)
	InfoContext(ctx context.Context, msg string, fields ...Field)
	WarnContext(ctx context.Context, msg string, fields ...Field)
	ErrorContext(ctx context.Context, msg string, fields ...Field)
	FatalContext(ctx context.Context, msg string, fields ...Field)

	// With 创建一个带有预设字段的子 Logger
	//
	// 预设的字段会出现在所有日志中。
	//
	// 示例：
	//   logger := logger.With(clog.String("user_id", "12345"))
	//   logger.Info("User logged in")
	//   // 输出：user_id=12345 msg="User logged in"
	With(fields ...Field) Logger

	// WithNamespace 创建一个扩展命名空间的子 Logger
	//
	// 命名空间会追加到现有的命名空间后面。
	//
	// 示例：
	//   logger := clog.WithNamespace("service", "api")
	//   handlerLogger := logger.WithNamespace("users")
	//   // 最终命名空间为 "service.api.users"
	WithNamespace(parts ...string) Logger

	// SetLevel 动态调整日志级别
	//
	// 允许运行时修改日志级别，不需要重新创建 Logger。
	//
	// 示例：
	//   logger.SetLevel(clog.DebugLevel)
	SetLevel(level Level) error

	// Flush 强制同步所有缓冲区的日志
	//
	// 确保所有日志都已写入输出目标。
	// 对于异步日志处理器特别有用。
	Flush()
}
