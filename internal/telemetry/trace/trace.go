package trace

import (
	"context"
	"fmt"

	"github.com/ceyewan/genesis/pkg/telemetry/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// tracer 使用 OpenTelemetry 的 Tracer 实现 types.Tracer 接口。
type tracer struct {
	t oteltrace.Tracer
}

// NewTracer 创建一个新的 Tracer 实例。
func NewTracer(t oteltrace.Tracer) types.Tracer {
	return &tracer{t: t}
}

// Start 启动一个新的 span。
func (t *tracer) Start(ctx context.Context, operationName string, opts ...types.TraceOption) (context.Context, types.Span) {
	options := &types.StartOptions{}
	for _, o := range opts {
		o(options)
	}

	var startOpts []oteltrace.SpanStartOption
	if options.Kind != types.SpanKindUnspecified {
		startOpts = append(startOpts, oteltrace.WithSpanKind(oteltrace.SpanKind(options.Kind)))
	}

	ctx, s := t.t.Start(ctx, operationName, startOpts...)
	return ctx, &span{s: s}
}

// span 使用 OpenTelemetry 的 Span 实现 types.Span 接口。
type span struct {
	s oteltrace.Span
}

// End 结束 span。
func (s *span) End() {
	s.s.End()
}

// SetStatus 设置 span 的状态。
func (s *span) SetStatus(code types.StatusCode, msg string) {
	var c codes.Code
	switch code {
	case types.StatusCodeOk:
		c = codes.Ok
	case types.StatusCodeError:
		c = codes.Error
	default:
		c = codes.Unset
	}
	s.s.SetStatus(c, msg)
}

// RecordError 记录 span 的一个错误事件。
func (s *span) RecordError(err error) {
	s.s.RecordError(err)
}

// SetAttributes 在 span 上设置属性。
func (s *span) SetAttributes(attrs ...types.Attribute) {
	s.s.SetAttributes(toAttributes(attrs)...)
}

// TraceID 返回 span 的跟踪 ID。
func (s *span) TraceID() string {
	return s.s.SpanContext().TraceID().String()
}

// SpanID 返回 span 的 span ID。
func (s *span) SpanID() string {
	return s.s.SpanContext().SpanID().String()
}

// toAttributes 将 types.Attribute 切片转换为 OpenTelemetry 的 attribute.KeyValue。
func toAttributes(attrs []types.Attribute) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	res := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		switch v := a.Value.(type) {
		case string:
			res[i] = attribute.String(a.Key, v)
		case int:
			res[i] = attribute.Int(a.Key, v)
		case int64:
			res[i] = attribute.Int64(a.Key, v)
		case float64:
			res[i] = attribute.Float64(a.Key, v)
		case bool:
			res[i] = attribute.Bool(a.Key, v)
		default:
			res[i] = attribute.String(a.Key, fmt.Sprintf("%v", v))
		}
	}
	return res
}
