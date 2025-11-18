package clog

import (
	"context"
	"log/slog"
	"os"
	"time"

	slogadapter "github.com/ceyewan/genesis/internal/clog/slog"
	"github.com/ceyewan/genesis/pkg/clog/types"
)

// loggerImpl 是Logger接口的具体实现
type loggerImpl struct {
	handler   slog.Handler
	config    *types.Config
	option    *types.Option
	baseAttrs []slog.Attr
}

// NewLogger 创建Logger实例（供pkg/clog调用）
func NewLogger(config *types.Config, option *types.Option) (types.Logger, error) {
	handler, err := slogadapter.NewHandler(config, option)
	if err != nil {
		return nil, err
	}

	logger := &loggerImpl{
		handler: handler,
		config:  config,
		option:  option,
	}

	logger.setupBaseAttrs()

	return logger, nil
}

func (l *loggerImpl) Debug(msg string, fields ...types.Field) {
	l.log(context.Background(), types.DebugLevel, msg, fields...)
}

func (l *loggerImpl) Info(msg string, fields ...types.Field) {
	l.log(context.Background(), types.InfoLevel, msg, fields...)
}

func (l *loggerImpl) Warn(msg string, fields ...types.Field) {
	l.log(context.Background(), types.WarnLevel, msg, fields...)
}

func (l *loggerImpl) Error(msg string, fields ...types.Field) {
	l.log(context.Background(), types.ErrorLevel, msg, fields...)
}

func (l *loggerImpl) Fatal(msg string, fields ...types.Field) {
	l.log(context.Background(), types.FatalLevel, msg, fields...)
}

func (l *loggerImpl) DebugContext(ctx context.Context, msg string, fields ...types.Field) {
	l.log(ctx, types.DebugLevel, msg, fields...)
}

func (l *loggerImpl) InfoContext(ctx context.Context, msg string, fields ...types.Field) {
	l.log(ctx, types.InfoLevel, msg, fields...)
}

func (l *loggerImpl) WarnContext(ctx context.Context, msg string, fields ...types.Field) {
	l.log(ctx, types.WarnLevel, msg, fields...)
}

func (l *loggerImpl) ErrorContext(ctx context.Context, msg string, fields ...types.Field) {
	l.log(ctx, types.ErrorLevel, msg, fields...)
}

func (l *loggerImpl) FatalContext(ctx context.Context, msg string, fields ...types.Field) {
	l.log(ctx, types.FatalLevel, msg, fields...)
}

func (l *loggerImpl) WithNamespace(parts ...string) types.Logger {
	newOption := *l.option
	newOption.NamespaceParts = append(l.option.NamespaceParts, parts...)

	newLogger := &loggerImpl{
		handler: l.handler,
		config:  l.config,
		option:  &newOption,
	}
	newLogger.setupBaseAttrs()

	return newLogger
}

func (l *loggerImpl) With(fields ...types.Field) types.Logger {
	// 创建builder并应用所有字段
	builder := &types.LogBuilder{
		Data: make(map[string]any),
	}

	for _, field := range fields {
		field(builder)
	}

	// 转换为slog.Attr
	attrs := make([]slog.Attr, 0, len(builder.Data))
	for k, v := range builder.Data {
		attrs = append(attrs, slog.Any(k, v))
	}

	// 创建新的logger实例
	newLogger := &loggerImpl{
		handler:   l.handler,
		config:    l.config,
		option:    l.option,
		baseAttrs: append(l.baseAttrs, attrs...),
	}

	return newLogger
}

// 内部方法
func (l *loggerImpl) log(ctx context.Context, level types.Level, msg string, fields ...types.Field) {
	builder := &types.LogBuilder{
		Data: make(map[string]any),
	}

	// 应用所有字段
	for _, field := range fields {
		field(builder)
	}

	// 提取Context字段、处理命名空间等
	extractContextFields(ctx, l.option, builder)
	addNamespaceFields(l.option, builder) // 只在log方法中添加一次

	// 转换为slog并记录
	attrs := l.convertToAttrs(builder)
	attrs = append(l.baseAttrs, attrs...)

	// 设置正确的调用者跳过级别 - 跳过当前函数调用栈
	record := slog.NewRecord(time.Now(), slog.Level(level), msg, 1)
	record.AddAttrs(attrs...)

	l.handler.Handle(ctx, record)

	if level == types.FatalLevel {
		os.Exit(1)
	}
}

// SetLevel 动态调整日志级别
func (l *loggerImpl) SetLevel(level types.Level) error {
	if h, ok := l.handler.(interface{ SetLevel(types.Level) error }); ok {
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

// setupBaseAttrs 初始化 logger 的基础属性，例如命名空间。
func (l *loggerImpl) setupBaseAttrs() {
	// 在 NewLogger 中调用，用于初始化 baseAttrs
	builder := &types.LogBuilder{
		Data: make(map[string]any),
	}
	// 不再在这里添加命名空间，避免重复
	// addNamespaceFields(l.option, builder)

	l.baseAttrs = l.convertToAttrs(builder)
}

// convertToAttrs 将 LogBuilder 中的数据转换为 slog.Attr 数组
func (l *loggerImpl) convertToAttrs(builder *types.LogBuilder) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(builder.Data))
	for k, v := range builder.Data {
		attrs = append(attrs, slog.Any(k, v))
	}
	return attrs
}
