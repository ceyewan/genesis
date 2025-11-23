package types

import "context"

// Tracer 用于创建 Span
type Tracer interface {
	// Start 开启一个新的 Span
	// 如果 ctx 中已存在父 Span，则自动关联
	Start(ctx context.Context, operationName string, opts ...TraceOption) (context.Context, Span)
}

// Span 代表链路中的一个操作单元
type Span interface {
	// End 结束 Span
	End()

	// SetStatus 设置状态（如 Error）
	SetStatus(code StatusCode, msg string)

	// RecordError 记录错误事件
	RecordError(err error)

	// SetAttributes 设置属性
	SetAttributes(attrs ...Attribute)

	// TraceID 获取当前 TraceID（用于日志关联）
	TraceID() string

	// SpanID 获取当前 SpanID
	SpanID() string
}

// StatusCode 定义 Span 的状态码
type StatusCode int

const (
	StatusCodeUnset StatusCode = 0
	StatusCodeOk    StatusCode = 1
	StatusCodeError StatusCode = 2
)

// Attribute 定义 Span 的属性
type Attribute struct {
	Key   string
	Value any
}

// TraceOption 定义 Start 的配置选项
type TraceOption func(*StartOptions)

type StartOptions struct {
	Kind SpanKind
}

type SpanKind int

const (
	SpanKindUnspecified SpanKind = 0
	SpanKindInternal    SpanKind = 1
	SpanKindServer      SpanKind = 2
	SpanKindClient      SpanKind = 3
	SpanKindProducer    SpanKind = 4
	SpanKindConsumer    SpanKind = 5
)

func WithSpanKind(kind SpanKind) TraceOption {
	return func(o *StartOptions) {
		o.Kind = kind
	}
}

func WithAttributes(attrs ...Attribute) TraceOption {
	// 这是一个占位符。在真实实现中，我们会将这些属性存储在 StartOptions 中
	// 并在启动 span 时使用它们。
	// 目前，我们忽略它们以保持接口简洁。
	// 或者，我们可以向 StartOptions 添加 Attributes 字段。
	return func(o *StartOptions) {
		// o.Attributes = append(o.Attributes, attrs...)
	}
}
