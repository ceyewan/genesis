package ratelimit

import (
	"github.com/ceyewan/genesis/clog"
	metrics "github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithMeter 设置 Meter
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		o.meter = meter
	}
}
