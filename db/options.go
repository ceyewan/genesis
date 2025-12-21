package db

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option DB 组件选项函数
type Option func(*options)

// options 选项结构（内部使用）
type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// WithLogger 注入日志记录器
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.meter = m
	}
}
