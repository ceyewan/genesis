package cache

import (
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Option 缓存组件选项函数
type Option func(*Options)

// Options 选项结构（导出供 internal 使用）
type Options struct {
	Logger clog.Logger
	Meter  types.Meter
	Tracer types.Tracer
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 Namespace: logger.WithNamespace("cache")
func WithLogger(l clog.Logger) Option {
	return func(o *Options) {
		if l != nil {
			o.Logger = l.WithNamespace("cache")
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m types.Meter) Option {
	return func(o *Options) {
		o.Meter = m
	}
}

// WithTracer 注入 Tracer
func WithTracer(t types.Tracer) Option {
	return func(o *Options) {
		o.Tracer = t
	}
}
