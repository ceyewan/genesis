package clog

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"
)

// loggerImpl 是Logger接口的具体实现
type loggerImpl struct {
	handler   slog.Handler
	config    *Config
	options   *options
	baseAttrs []slog.Attr
}

// newLogger 创建Logger实例（内部使用）
func newLogger(config *Config, options *options) (Logger, error) {
	handler, err := newHandler(config, options)
	if err != nil {
		return nil, err
	}

	logger := &loggerImpl{
		handler: handler,
		config:  config,
		options: options,
	}

	logger.setupBaseAttrs()

	return logger, nil
}

func (l *loggerImpl) Debug(msg string, fields ...Field) {
	l.log(context.Background(), DebugLevel, msg, fields...)
}

func (l *loggerImpl) Info(msg string, fields ...Field) {
	l.log(context.Background(), InfoLevel, msg, fields...)
}

func (l *loggerImpl) Warn(msg string, fields ...Field) {
	l.log(context.Background(), WarnLevel, msg, fields...)
}

func (l *loggerImpl) Error(msg string, fields ...Field) {
	l.log(context.Background(), ErrorLevel, msg, fields...)
}

func (l *loggerImpl) Fatal(msg string, fields ...Field) {
	l.log(context.Background(), FatalLevel, msg, fields...)
}

func (l *loggerImpl) DebugContext(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, DebugLevel, msg, fields...)
}

func (l *loggerImpl) InfoContext(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, InfoLevel, msg, fields...)
}

func (l *loggerImpl) WarnContext(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, WarnLevel, msg, fields...)
}

func (l *loggerImpl) ErrorContext(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, ErrorLevel, msg, fields...)
}

func (l *loggerImpl) FatalContext(ctx context.Context, msg string, fields ...Field) {
	l.log(ctx, FatalLevel, msg, fields...)
}

func (l *loggerImpl) WithNamespace(parts ...string) Logger {
	newOptions := *l.options
	newOptions.namespaceParts = append(l.options.namespaceParts, parts...)

	newLogger := &loggerImpl{
		handler: l.handler,
		config:  l.config,
		options: &newOptions,
	}
	newLogger.setupBaseAttrs()

	return newLogger
}

func (l *loggerImpl) With(fields ...Field) Logger {
	// 直接将 slog.Attr 字段追加到 baseAttrs
	newLogger := &loggerImpl{
		handler:   l.handler,
		config:    l.config,
		options:   l.options,
		baseAttrs: append(l.baseAttrs, fields...),
	}

	return newLogger
}

// 内部方法
func (l *loggerImpl) log(ctx context.Context, level Level, msg string, fields ...Field) {
	// 准备属性切片：baseAttrs + fields + contextFields + namespaceFields
	attrs := make([]slog.Attr, 0, len(l.baseAttrs)+len(fields)+4)
	attrs = append(attrs, l.baseAttrs...)
	attrs = append(attrs, fields...)

	// 提取Context字段、处理命名空间等
	extractContextFields(ctx, l.options, &attrs)
	addNamespaceFields(l.options, &attrs) // 只在log方法中添加一次

	// 将 Level 映射为 slog.Level，避免直接按数字转换导致不一致
	var slogLevel slog.Level
	switch level {
	case DebugLevel:
		slogLevel = slog.LevelDebug
	case InfoLevel:
		slogLevel = slog.LevelInfo
	case WarnLevel:
		slogLevel = slog.LevelWarn
	case ErrorLevel:
		slogLevel = slog.LevelError
	case FatalLevel:
		// Fatal 在 slog 中没有显式常量，使用 Error 的更高值
		slogLevel = slog.LevelError + 4
	default:
		slogLevel = slog.LevelInfo
	}

	// 获取正确的程序计数器(PC)值，用于准确的源码位置
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip: runtime.Callers, logger.log, Debug/Info/Error等
	record := slog.NewRecord(time.Now(), slogLevel, msg, pcs[0])
	record.AddAttrs(attrs...)

	// 使用 handler.Enabled 进行级别检查，避免直接调用 Handle 绕过过滤逻辑
	if enabled := l.handler.Enabled(ctx, slogLevel); !enabled {
		return
	}

	err := l.handler.Handle(ctx, record)
	if err != nil {
		// 处理日志处理错误（可选）
		return
	}

	if level == FatalLevel {
		os.Exit(1)
	}
}

// SetLevel 动态调整日志级别
//
// TODO: 目前未实现真正的动态级别切换。slog.Handler 的 Level 在创建时固定，
// 运行时修改需要包装 Handler 并使用 atomic.Value 或互斥锁保护级别变量。
// 当前实现仅作为接口兼容占位。
func (l *loggerImpl) SetLevel(level Level) error {
	if h, ok := l.handler.(interface{ SetLevel(Level) error }); ok {
		return h.SetLevel(level)
	}
	return nil // 无法动态调整，忽略错误
}

// Flush 强制同步所有缓冲区的日志
func (l *loggerImpl) Flush() {
	if h, ok := l.handler.(interface{ Flush() }); ok {
		h.Flush()
	}
}

// setupBaseAttrs 初始化 logger 的基础属性
func (l *loggerImpl) setupBaseAttrs() {
	// 创建空的 baseAttrs
	l.baseAttrs = []slog.Attr{}
}
