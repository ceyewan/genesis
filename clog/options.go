package clog

import (
	"bytes"
	"reflect"
	"strings"

	"github.com/ceyewan/genesis/xerrors"
)

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

// WithTraceContext 开启 OpenTelemetry TraceID 自动提取。
//
// 启用后，每次记录日志时会从 Context 中提取 OTel 的 TraceID 和 SpanID，
// 并自动注入到结构化日志字段中。
//
// 生效需要同时满足两个前置条件，缺一则 trace_id / span_id 字段静默为空，不报错：
//  1. 应用启动时已调用 [trace.Init]，安装了全局 TracerProvider
//  2. 请求路径上有活跃 Span：HTTP 路由注册了 trace.GinMiddleware()，
//     或 gRPC 服务注册了 trace.GRPCServerStatsHandler()
//
// 推荐接入顺序：trace.Init → clog.New(WithTraceContext) → 注册中间件。
// 完整示例见 docs/genesis-observability-blog.md。
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
		if opt != nil {
			opt(o)
		}
	}

	return o
}

func validateOptions(o *options) error {
	for _, field := range o.contextFields {
		if field.Key == nil {
			return xerrors.Wrap(ErrInvalidConfig, "context field key must not be nil")
		}
		if !reflect.TypeOf(field.Key).Comparable() {
			return xerrors.Wrap(ErrInvalidConfig, "context field key must be comparable")
		}
		if strings.TrimSpace(field.FieldName) == "" {
			return xerrors.Wrap(ErrInvalidConfig, "context field name must not be blank")
		}
	}
	return nil
}
