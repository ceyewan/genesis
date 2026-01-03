package clog

import "bytes"

// ContextField 定义从 Context 中提取字段的规则
type ContextField struct {
	Key       any    // Context 中存储的键
	FieldName string // 日志中的字段名
}

// Option 函数式选项，用于配置 Logger 实例
type Option func(*options)

// options 内部选项结构，存储 Logger 的配置选项
type options struct {
	namespaceParts        []string
	contextFields         []ContextField
	buffer                *bytes.Buffer // 测试用缓冲区
	enableTraceExtraction bool
}

// WithNamespace 设置日志命名空间，支持多级命名空间
//
// 命名空间会以 "." 连接，作为日志中的 namespace 字段。
//
// 示例：
//
//	// 设置为 "order-service.api"
//	clog.WithNamespace("order-service", "api")
func WithNamespace(parts ...string) Option {
	return func(o *options) {
		o.namespaceParts = append(o.namespaceParts, parts...)
	}
}

// WithContextField 添加自定义的 Context 字段提取规则
//
// 可以从 Context 中提取任意字段并添加到日志中。
//
// 示例：
//
//	clog.WithContextField("trace-id", "trace_id")
func WithContextField(key any, fieldName string) Option {
	return func(o *options) {
		o.contextFields = append(o.contextFields, ContextField{
			Key:       key,
			FieldName: fieldName,
		})
	}
}

// WithStandardContext 自动提取标准的上下文字段
//
// 这是一个便捷方法，会自动添加以下常用字段的提取规则：
//   - trace_id: 追踪标识
//   - user_id: 用户标识
//   - request_id: 请求标识
//
// 示例：
//
//	ctx := context.WithValue(context.Background(), "trace_id", "abc123")
//	logger.WithStandardContext().InfoContext(ctx, "Request processed")
//	// 日志中会包含：trace_id=abc123
func WithStandardContext() Option {
	return func(o *options) {
		o.contextFields = append(o.contextFields,
			ContextField{Key: "trace_id", FieldName: "trace_id"},
			ContextField{Key: "user_id", FieldName: "user_id"},
			ContextField{Key: "request_id", FieldName: "request_id"},
		)
	}
}

// WithTraceContext 开启 OpenTelemetry TraceID 自动提取
//
// 启用后，会自动从 Context 中提取 OTel 的 TraceID 和 SpanID。
func WithTraceContext() Option {
	return func(o *options) {
		o.enableTraceExtraction = true
	}
}

// applyOptions 应用所有选项并返回配置（内部使用）
func applyOptions(opts ...Option) *options {
	o := &options{
		namespaceParts: []string{},
		contextFields:  []ContextField{},
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}
