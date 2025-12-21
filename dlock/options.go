package dlock

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option DLock 组件初始化选项函数
type Option func(*options)

// options 选项结构（内部使用，小写）
type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// WithLogger 注入日志记录器
// 组件会自动添加 component=dlock 字段
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithMeter 注入指标收集器
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.meter = m
	}
}
