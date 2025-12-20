package idgen

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*Options)

// Options 组件初始化选项配置
type Options struct {
	Logger clog.Logger
	Meter  metrics.Meter
	Tracer interface{} // TODO: 实现 Tracer 接口，暂时使用 interface{}
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *Options) {
		o.Logger = logger
	}
}

// WithMeter 设置 Meter
func WithMeter(meter metrics.Meter) Option {
	return func(o *Options) {
		o.Meter = meter
	}
}

// WithTracer 设置 Tracer (TODO: 待实现 Tracer 接口)
func WithTracer(tracer interface{}) Option {
	return func(o *Options) {
		o.Tracer = tracer
	}
}
