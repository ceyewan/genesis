package db

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option DB 组件选项函数
type Option func(*Options)

// Options 选项结构（导出供内部使用）
type Options struct {
	Logger clog.Logger
	Meter  metrics.Meter
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 Namespace: logger.WithNamespace("db")
func WithLogger(l clog.Logger) Option {
	return func(o *Options) {
		if l != nil {
			o.Logger = l.WithNamespace("db")
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *Options) {
		o.Meter = m
	}
}
