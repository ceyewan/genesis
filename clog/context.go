package clog

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// extractContextFields 从 context 中提取配置的字段，并追加到 attrs 切片中
func extractContextFields(ctx context.Context, options *options, attrs *[]slog.Attr) {
	if ctx == nil || options == nil {
		return
	}

	// 1. 处理 OTel TraceID 提取
	if options.enableTraceExtraction {
		span := trace.SpanFromContext(ctx)
		if span.SpanContext().IsValid() {
			*attrs = append(*attrs,
				slog.String("trace_id", span.SpanContext().TraceID().String()),
				slog.String("span_id", span.SpanContext().SpanID().String()),
			)
		}
	}

	// 2. 处理通用字段提取
	if len(options.contextFields) > 0 {
		for _, cf := range options.contextFields {
			val := ctx.Value(cf.Key)
			if val != nil {
				*attrs = append(*attrs, slog.Any(cf.FieldName, val))
			}
		}
	}
}
