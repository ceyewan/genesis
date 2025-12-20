// Package clog 提供基于 slog 的结构化日志组件，支持 Context 字段提取和命名空间管理。
//
// 基本使用：
//
//	logger, _ := clog.New(&clog.Config{
//	    Level:  "info",
//	    Format: "console",
//	    Output: "stdout",
//	})
//	logger.Info("Hello, World!", clog.String("key", "value"))
//
// 使用函数式选项：
//
//	logger, _ := clog.New(&clog.Config{Level: "info"},
//	    clog.WithNamespace("my-service", "api"),
//	    clog.WithStandardContext(), // 自动提取 trace_id, user_id, request_id
//	)
//
// 带 Context 的日志：
//
//	ctx := context.WithValue(context.Background(), "trace-id", "abc123")
//	logger.InfoContext(ctx, "Request processed")
package clog

import "bytes"

// ContextField 定义从 Context 中提取字段的规则
//
// 示例：
//
//	ContextField{
//	    Key: "trace-id",           // Context 中的键
//	    FieldName: "trace_id",    // 日志中的字段名
//	    Required: true,           // 是否为必需字段
//	}
type ContextField struct {
	Key       any                   // Context 中存储的键
	FieldName string                // 输出的最终字段名，如 "trace_id"
	Required  bool                  // 是否必须存在
	Extract   func(any) (any, bool) // 可选的自定义提取函数
}

// Option 函数式选项，用于配置 Logger 实例
//
// 示例：
//
//	clog.WithNamespace("service", "module")  // 设置命名空间为 "service.module"
//	clog.WithStandardContext()              // 自动提取标准上下文字段
//	clog.WithContextField("key", "field", clog.Required(true))
type Option func(*options)

// options 内部选项结构，存储 Logger 的配置选项
type options struct {
	namespaceParts  []string
	contextFields   []ContextField
	contextPrefix   string
	namespaceJoiner string
	buffer          *bytes.Buffer // 测试用缓冲区
}

// WithNamespace 设置日志命名空间，支持多级命名空间
//
// 命名空间会以 "." 连接，作为日志中的 namespace 字段。
//
// 示例：
//
//	// 设置为 "order-service.api"
//	clog.WithNamespace("order-service", "api")
//
//	// 可以链式使用：WithNamespace("order").WithNamespace("api")
//	// 结果为 "order.api"
func WithNamespace(parts ...string) Option {
	return func(o *options) {
		o.namespaceParts = append(o.namespaceParts, parts...)
	}
}

// WithContextPrefix 设置从 Context 提取字段时的前缀
//
// 默认前缀为 "ctx."，即 Context 中的字段会以 "ctx.xxx" 的形式出现在日志中。
//
// 示例：
//
//	// 不使用前缀
//	clog.WithContextPrefix("")
//
//	// 使用自定义前缀
//	clog.WithContextPrefix("meta.")
func WithContextPrefix(prefix string) Option {
	return func(o *options) {
		o.contextPrefix = prefix
	}
}

// WithNamespaceJoiner 设置命名空间各部分之间的连接符
//
// 默认连接符为 "."。
//
// 示例：
//
//	clog.WithNamespace("service", "api"), clog.WithNamespaceJoiner("-")
//	// 结果为 "service-api"
func WithNamespaceJoiner(joiner string) Option {
	return func(o *options) {
		o.namespaceJoiner = joiner
	}
}

// WithContextField 添加自定义的 Context 字段提取规则
//
// 可以从 Context 中提取任意字段并添加到日志中。
//
// 示例：
//
//	// 提取必需字段
//	clog.WithContextField("trace-id", "trace_id", clog.Required(true))
//
//	// 提取可选字段，并设置前缀
//	clog.WithContextField("user", "user_name", clog.WithContextPrefix("meta."))
//
//	// 使用自定义提取函数
//	clog.WithContextField("custom", "data", clog.WithExtractor(func(v any) (any, bool) {
//	    // 自定义逻辑
//	    return v, v != nil
//	}))
func WithContextField(key any, fieldName string, opts ...ContextFieldOption) Option {
	return func(o *options) {
		cf := ContextField{
			Key:       key,
			FieldName: fieldName,
		}
		// 应用选项
		for _, opt := range opts {
			opt(&cf)
		}
		o.contextFields = append(o.contextFields, cf)
	}
}

// ContextFieldOption Context 字段的配置选项
type ContextFieldOption func(*ContextField)

// Required 设置字段为必需字段
//
// 当 Context 中不存在该字段时，会记录警告（如果需要）。
//
// 示例：
//
//	clog.Required(true)   // 设置为必需
//	clog.Required(false)  // 设置为可选（默认）
func Required(required bool) ContextFieldOption {
	return func(cf *ContextField) {
		cf.Required = required
	}
}

// WithExtractor 设置自定义的字段提取函数
//
// 当 Context 中存储的值需要转换时使用。
//
// 示例：
//
//	clog.WithExtractor(func(v any) (any, bool) {
//	    if s, ok := v.(string); ok {
//	        return strings.ToUpper(s), true
//	    }
//	    return nil, false
//	})
func WithExtractor(extractor func(any) (any, bool)) ContextFieldOption {
	return func(cf *ContextField) {
		cf.Extract = extractor
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
//	// 日志中会包含：ctx.trace_id=abc123
func WithStandardContext() Option {
	return func(o *options) {
		o.contextFields = append(o.contextFields,
			ContextField{Key: "trace_id", FieldName: "trace_id", Required: false},
			ContextField{Key: "user_id", FieldName: "user_id", Required: false},
			ContextField{Key: "request_id", FieldName: "request_id", Required: false},
		)
	}
}

// applyOptions 应用所有选项并返回配置（内部使用）
func applyOptions(opts ...Option) *options {
	o := &options{
		contextPrefix:   "ctx.", // 默认前缀
		namespaceJoiner: ".",    // 默认连接符
		namespaceParts:  []string{},
		contextFields:   []ContextField{},
	}

	for _, opt := range opts {
		opt(o)
	}

	return o
}
