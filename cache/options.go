package cache

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 缓存组件选项函数
type Option func(*Options)

// Options 选项结构（导出供 internal 使用）
type Options struct {
	Logger clog.Logger
	Meter  metrics.Meter
	Tracer interface{} // TODO: 实现 Tracer 接口
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
func WithMeter(m metrics.Meter) Option {
	return func(o *Options) {
		o.Meter = m
	}
}

// WithTracer 注入 Tracer (TODO: 待实现)
func WithTracer(t interface{}) Option {
	return func(o *Options) {
		o.Tracer = t
	}
}
