package breaker

import (
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/metrics"
)

// Option 组件初始化选项函数
type Option func(*Options)

// Options 组件初始化选项配置
type Options struct {
	Logger clog.Logger
	Meter  metrics.Meter
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
