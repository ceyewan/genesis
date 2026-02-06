package trace

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// MessagingMeta 描述标准化的消息属性
type MessagingMeta struct {
	System        string
	Destination   string
	Operation     string
	ConsumerGroup string
	// TraceRelation 控制消费端与上游生产端的关系建模方式，默认 link。
	TraceRelation MessagingTraceRelation
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func normalizeTracer(tracer oteltrace.Tracer) oteltrace.Tracer {
	if tracer == nil {
		return otel.Tracer("genesis.trace")
	}
	return tracer
}

func messagingAttributes(meta MessagingMeta, attrs ...attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attrs)+4)
	if meta.System != "" {
		out = append(out, attribute.String(AttrMessagingSystem, meta.System))
	}
	if meta.Destination != "" {
		out = append(out, attribute.String(AttrMessagingDestination, meta.Destination))
	}
	if meta.Operation != "" {
		out = append(out, attribute.String(AttrMessagingOperation, meta.Operation))
	}
	if meta.ConsumerGroup != "" {
		out = append(out, attribute.String(AttrMessagingConsumerGroup, meta.ConsumerGroup))
	}
	out = append(out, attrs...)
	return out
}

// StartProducerSpan 启动一个标准化的生产者 Span，并将上下文注入到 headers
func StartProducerSpan(
	ctx context.Context,
	tracer oteltrace.Tracer,
	spanName string,
	meta MessagingMeta,
	attrs ...attribute.KeyValue,
) (context.Context, oteltrace.Span, map[string]string) {
	ctx = normalizeContext(ctx)
	tracer = normalizeTracer(tracer)

	spanCtx, span := tracer.Start(ctx, spanName, oteltrace.WithSpanKind(oteltrace.SpanKindProducer))
	span.SetAttributes(messagingAttributes(meta, attrs...)...)

	headers := map[string]string{}
	Inject(spanCtx, headers)
	return spanCtx, span, headers
}

// StartConsumerSpanFromHeaders 从传入的 headers 启动一个标准化的消费者 Span
// 关系默认是 link，可通过 MessagingMeta.TraceRelation 切换为 child_of
func StartConsumerSpanFromHeaders(
	ctx context.Context,
	tracer oteltrace.Tracer,
	spanName string,
	headers map[string]string,
	meta MessagingMeta,
	attrs ...attribute.KeyValue,
) (context.Context, oteltrace.Span) {
	ctx = normalizeContext(ctx)
	tracer = normalizeTracer(tracer)

	var extracted context.Context
	if len(headers) > 0 {
		extracted = Extract(ctx, headers)
	} else {
		extracted = ctx
	}

	relation := meta.TraceRelation
	if relation == "" {
		relation = MessagingTraceRelationLink
	}

	spanCtxForStart := ctx
	startOpts := []oteltrace.SpanStartOption{oteltrace.WithSpanKind(oteltrace.SpanKindConsumer)}

	if remoteSC := oteltrace.SpanContextFromContext(extracted); remoteSC.IsValid() {
		switch relation {
		case MessagingTraceRelationChildOf:
			spanCtxForStart = extracted
		case MessagingTraceRelationLink:
			startOpts = append(startOpts, oteltrace.WithLinks(oteltrace.Link{SpanContext: remoteSC}))
		default:
			startOpts = append(startOpts, oteltrace.WithLinks(oteltrace.Link{SpanContext: remoteSC}))
		}
	}

	spanCtx, span := tracer.Start(
		spanCtxForStart,
		spanName,
		startOpts...,
	)
	span.SetAttributes(messagingAttributes(meta, attrs...)...)
	return spanCtx, span
}

// MarkSpanError 记录并将 Span 标记为错误，当 err 不为 nil 时
func MarkSpanError(span oteltrace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
