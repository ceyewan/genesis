package clog

import (
	"context"
	"strings"
)

// extractContextFields 从 context 中提取配置的字段，并添加到 map 中。
func extractContextFields(ctx context.Context, options *options, data map[string]any) {
	if ctx == nil || options == nil || len(options.contextFields) == 0 {
		return
	}

	prefix := options.contextPrefix

	for _, cf := range options.contextFields {
		val := ctx.Value(cf.Key)

		if val == nil {
			if cf.Required {
				// 如果是必需字段但不存在，可以考虑记录一个内部警告，但这里我们选择跳过
				continue
			}
			continue
		}

		var extractedVal any
		var ok bool

		if cf.Extract != nil {
			// 使用自定义提取函数
			extractedVal, ok = cf.Extract(val)
		} else {
			// 直接使用值
			extractedVal = val
			ok = true
		}

		if ok {
			fieldName := cf.FieldName
			if !strings.HasPrefix(fieldName, prefix) {
				fieldName = prefix + fieldName
			}
			data[fieldName] = extractedVal
		}
	}
}
