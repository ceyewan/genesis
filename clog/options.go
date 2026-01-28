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
func WithNamespace(parts ...string) Option {
	return func(o *options) {
		o.namespaceParts = append(o.namespaceParts, parts...)
	}
}

// WithContextField 添加自定义的 Context 字段提取规则
//
// 可以从 Context 中提取任意字段并添加到日志中。
// 推荐常用字段：trace_id、user_id、request_id
// 如果开启了 OpenTelemetry TraceID 提取，则无需手动添加 trace_id 字段。
func WithContextField(key any, fieldName string) Option {
	return func(o *options) {
		o.contextFields = append(o.contextFields, ContextField{
			Key:       key,
			FieldName: fieldName,
		})
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
