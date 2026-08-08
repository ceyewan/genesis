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

// applyDefaults 确保未设置的选项使用默认值
func (o *options) applyDefaults() {
	if o.logger == nil {
		o.logger = clog.Discard()
	}
	if o.meter == nil {
		o.meter = metrics.Discard()
	}
}

// WithMeter 注入 connector 内部健康检查和重连指标。
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		if meter == nil {
			meter = metrics.Discard()
		}
		o.meter = meter
	}
}

func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger == nil {
			logger = clog.Discard()
		}
		o.logger = logger.WithNamespace("connector")
	}
}
