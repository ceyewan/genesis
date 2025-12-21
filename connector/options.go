package connector

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// Option 配置连接器的选项
type Option func(*options)

// WithLogger 设置日志记录器
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.logger = logger.WithNamespace("connector")
	}
}

// WithMeter 设置指标收集器
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		o.meter = meter
	}
}
