package clog

import (
	"context"
	"log/slog"
)

// extractContextFields 从 context 中提取配置的字段，并追加到 attrs 切片中
func extractContextFields(ctx context.Context, options *options, attrs *[]slog.Attr) {
	if ctx == nil || options == nil || len(options.contextFields) == 0 {
		return
	}

	for _, cf := range options.contextFields {
		val := ctx.Value(cf.Key)
		if val != nil {
			*attrs = append(*attrs, slog.Any(cf.FieldName, val))
		}
	}
}
