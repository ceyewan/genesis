package log

import (
	"context"
	"log"
	"os"
	"sync"
	"sync/atomic"

	clog "github.com/ceyewan/genesis/internal/log"
	"go.uber.org/zap"
)

// Logger 定义统一的日志记录接口，封装 zap.Logger 提供类型安全的使用方式
type Logger = clog.Logger

var (
	// defaultLogger 全局默认日志器，使用 atomic.Value 保证并发安全
	defaultLogger atomic.Value

	// defaultLoggerOnce 确保默认日志器只初始化一次
	defaultLoggerOnce sync.Once

	// exitFunc 退出函数，支持测试时进行 mock
	exitFunc = os.Exit

	// traceIDKey 类型安全的上下文键，避免字符串键冲突
	traceIDKey struct{}
)

// SetExitFunc 设置退出函数，用于测试时模拟 os.Exit 行为
// 调用此函数后，Fatal 日志将调用指定的函数而非直接退出程序
func SetExitFunc(fn func(int)) {
	exitFunc = fn
	clog.SetExitFunc(fn)
}

// WithTraceID 将 trace_id 注入到 context 中，返回新的 context
// 通常在请求入口处调用，如 HTTP 中间件或 gRPC 拦截器
// 注入的 trace_id 会被 WithContext 自动提取并添加到日志中
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithContext 从 context 中获取 Logger 实例
// 如果 ctx 中包含 trace_id，返回的 Logger 会自动在每条日志中添加 "trace_id" 字段
// 这是业务代码中进行日志记录的首选方式，确保分布式链路追踪的连续性
func WithContext(ctx context.Context) Logger {
	logger := getDefaultLogger()

	if ctx != nil {
		if traceID := ctx.Value(traceIDKey); traceID != nil {
			if id, ok := traceID.(string); ok && id != "" {
				return logger.With(zap.String("trace_id", id))
			}
		}
	}

	return logger
}

// getDefaultLogger 获取全局默认日志器
// 使用延迟初始化模式，第一次调用时创建并缓存实例
// 初始化失败时会创建 fallback logger 确保系统可用性
func getDefaultLogger() Logger {
	defaultLoggerOnce.Do(func() {
		cfg := GetDefaultConfig("development")
		logger, err := clog.NewLogger(cfg, "")
		if err != nil {
			// 初始化失败时至少在标准错误中打印错误信息
			log.Printf("clog: failed to initialize default logger: %v", err)
			logger = clog.NewFallbackLogger()
		}
		defaultLogger.Store(logger)
	})
	return defaultLogger.Load().(Logger)
}

// New 创建独立的 Logger 实例，支持自定义配置
// 适用于需要特殊日志配置的场景，如不同输出位置或格式
//
// 参数：
//   - ctx: 控制初始化过程的上下文，Logger 不持有此上下文
//   - config: 日志配置，必须通过 Validate() 验证
//   - opts: 功能选项，如 WithNamespace() 设置命名空间
//
// 返回：
//   - Logger: 配置好的日志实例
//   - error: 配置无效时的错误，或初始化警告
func New(ctx context.Context, config *Config, opts ...Option) (Logger, error) {
	// 验证配置有效性
	if err := config.Validate(); err != nil {
		// 配置无效时直接返回错误，不创建 logger
		return nil, err
	}

	// 解析选项
	options := ParseOptions(opts...)
	logger, err := clog.NewLogger(config, options.Namespace)
	if err != nil {
		// 初始化失败时返回 fallback logger 和原始错误
		return clog.NewFallbackLogger(), err
	}
	return logger, nil
}

// Init 初始化全局默认日志器
// 这是最常用的初始化方式，通常在服务的 main 函数中调用一次
//
// 参数：
//   - ctx: 控制初始化过程的上下文，Logger 不持有此上下文
//   - config: 日志配置，必须通过 Validate() 验证
//   - opts: 功能选项，如 WithNamespace() 设置服务命名空间
//
// 返回：
//   - error: 配置无效或初始化失败时的错误
//
// 注意：
//   - 初始化失败时不会替换现有 logger，保持系统可用性
//   - 重复调用会原子替换现有全局 logger
func Init(ctx context.Context, config *Config, opts ...Option) error {
	// 验证配置有效性
	if err := config.Validate(); err != nil {
		return err
	}

	// 解析选项
	options := ParseOptions(opts...)
	logger, err := clog.NewLogger(config, options.Namespace)
	if err != nil {
		// 初始化失败时返回错误，但不替换现有 logger
		return err
	}
	// 原子替换全局 logger
	defaultLogger.Store(logger)
	return nil
}

// Namespace 创建带有层次化命名空间的 Logger 实例
// 支持链式调用来构建深层命名空间路径，如 "service.module.component"
// 这是区分不同业务模块或分层的推荐方式
//
// 示例：
//
//	userLogger := clog.Namespace("user")
//	authLogger := userLogger.Namespace("auth")  // "user.auth"
//	dbLogger := authLogger.Namespace("database") // "user.auth.database"
func Namespace(name string) Logger {
	return getDefaultLogger().Namespace(name)
}

// Debug 记录 Debug 级别的日志
// 通常用于详细的调试信息，在生产环境中通常被禁用
func Debug(msg string, fields ...Field) {
	getDefaultLogger().WithOptions(zap.AddCallerSkip(1)).Debug(msg, fields...)
}

// Info 记录 Info 级别的日志
// 用于记录一般的业务信息，如请求处理、状态变更等
func Info(msg string, fields ...Field) {
	getDefaultLogger().WithOptions(zap.AddCallerSkip(1)).Info(msg, fields...)
}

// Warn 记录 Warn 级别的日志
// 用于记录可能需要注意但不影响系统正常运行的情况
func Warn(msg string, fields ...Field) {
	getDefaultLogger().WithOptions(zap.AddCallerSkip(1)).Warn(msg, fields...)
}

// Error 记录 Error 级别的日志
// 用于记录错误情况，但不影响系统继续运行
func Error(msg string, fields ...Field) {
	getDefaultLogger().WithOptions(zap.AddCallerSkip(1)).Error(msg, fields...)
}

// Fatal 记录 Fatal 级别的日志并退出程序
// 用于记录严重错误，系统无法继续运行的情况
// 记录日志后会调用 exitFunc(1) 退出程序
func Fatal(msg string, fields ...Field) {
	getDefaultLogger().WithOptions(zap.AddCallerSkip(1)).Fatal(msg, fields...)
	exitFunc(1)
}
