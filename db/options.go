package db

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 配置 DB 实例的选项
type Option func(*options)

// options 内部选项结构
type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// WithLogger 注入日志记录器
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("db")
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		if m != nil {
			o.meter = m
		}
	}
}
