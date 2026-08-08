package clog

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type loggerSharedState struct {
	handler   slog.Handler
	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
}

// loggerImpl 是Logger接口的具体实现
type loggerImpl struct {
	shared        *loggerSharedState
	config        *Config
	options       *options
	baseAttrs     []slog.Attr
	ownsResources bool
}

// newLogger 创建Logger实例（内部使用）
func newLogger(config *Config, options *options) (Logger, error) {
	handler, err := newHandler(config, options)
	if err != nil {
		return nil, err
	}

	logger := &loggerImpl{
		shared: &loggerSharedState{
			handler: handler,
		},
		config:        config,
		options:       options,
		ownsResources: true,
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
	newOptions.namespaceParts = append([]string(nil), l.options.namespaceParts...)
	newOptions.namespaceParts = append(newOptions.namespaceParts, parts...)

	newLogger := &loggerImpl{
		shared:        l.shared,
		config:        l.config,
		options:       &newOptions,
		baseAttrs:     append([]slog.Attr(nil), l.baseAttrs...),
		ownsResources: false,
	}

	return newLogger
}

func (l *loggerImpl) With(fields ...Field) Logger {
	// 直接将 slog.Attr 字段追加到 baseAttrs。
	//
	// 注意：这里必须复制 baseAttrs，避免派生 Logger 之间共享底层数组导致字段互相覆盖。
	// 例如：
	//   base := logger.With(a).With(b)  // baseAttrs 可能有多余 cap
	//   c1 := base.With(x)
	//   c2 := base.With(y)             // 若不复制，可能覆盖 c1 的 x
	baseAttrs := append([]slog.Attr(nil), l.baseAttrs...)
	baseAttrs = append(baseAttrs, fields...)

	newLogger := &loggerImpl{
		shared:        l.shared,
		config:        l.config,
		options:       l.options,
		baseAttrs:     baseAttrs,
		ownsResources: false,
	}

	return newLogger
}

// 内部方法
func (l *loggerImpl) log(ctx context.Context, level Level, msg string, fields ...Field) {
	if l.shared == nil || l.shared.closed.Load() {
		return
	}

	slogLevel, err := slogLevelFromLevel(level)
	if err != nil {
		return
	}

	handler := l.shared.handler
	if enabled := handler.Enabled(ctx, slogLevel); !enabled {
		return
	}

	// 准备属性切片：baseAttrs + fields + contextFields + namespaceFields
	attrs := make([]slog.Attr, 0, len(l.baseAttrs)+len(fields)+4)
	attrs = append(attrs, l.baseAttrs...)
	attrs = append(attrs, fields...)

	// 提取Context字段、处理命名空间等
	extractContextFields(ctx, l.options, &attrs)
	addNamespaceFields(l.options, &attrs) // 只在log方法中添加一次

	// 获取正确的程序计数器(PC)值，用于准确的源码位置
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // skip: runtime.Callers, logger.log, Debug/Info/Error等
	record := slog.NewRecord(time.Now(), slogLevel, msg, pcs[0])
	record.AddAttrs(attrs...)

	err = handler.Handle(ctx, record)
	if err != nil {
		// 处理日志处理错误（可选）
		return
	}
}

// SetLevel 动态调整日志级别
//
// 通过底层 handler 的 SetLevel 实现（基于 slog.LevelVar），可在运行时生效。
func (l *loggerImpl) SetLevel(level Level) error {
	if l.shared == nil || l.shared.closed.Load() {
		return nil
	}
	if h, ok := l.shared.handler.(interface{ SetLevel(Level) error }); ok {
		return h.SetLevel(level)
	}
	return nil // 无法动态调整，忽略错误
}

// Flush 强制同步所有缓冲区的日志
func (l *loggerImpl) Flush() {
	if l.shared == nil || l.shared.closed.Load() {
		return
	}
	if h, ok := l.shared.handler.(interface{ Flush() }); ok {
		h.Flush()
	}
}

// Close 释放 Logger 持有的底层资源。
func (l *loggerImpl) Close() error {
	if !l.ownsResources || l.shared == nil {
		return nil
	}

	l.shared.closeOnce.Do(func() {
		l.shared.closed.Store(true)
		if h, ok := l.shared.handler.(interface{ Close() error }); ok {
			l.shared.closeErr = h.Close()
		}
	})

	return l.shared.closeErr
}

// setupBaseAttrs 初始化 logger 的基础属性
func (l *loggerImpl) setupBaseAttrs() {
	l.baseAttrs = make([]slog.Attr, 0, 4)
	if l.config.ServiceName != "" {
		l.baseAttrs = append(l.baseAttrs, slog.String("service.name", l.config.ServiceName))
	}
	if l.config.Version != "" {
		l.baseAttrs = append(l.baseAttrs, slog.String("service.version", l.config.Version))
	}
	if l.config.InstanceID != "" {
		l.baseAttrs = append(l.baseAttrs, slog.String("service.instance.id", l.config.InstanceID))
	}
	if l.config.Environment != "" {
		l.baseAttrs = append(l.baseAttrs, slog.String("deployment.environment", l.config.Environment))
	}
}
