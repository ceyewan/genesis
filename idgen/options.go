package idgen

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 组件初始化选项配置（内部使用）
type options struct {
	Logger clog.Logger
	Meter  metrics.Meter
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.Logger = logger
	}
}

// WithMeter 设置 Meter
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		o.Meter = meter
	}
}
