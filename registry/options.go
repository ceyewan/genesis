package registry

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 选项结构
type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// WithMeter 注入 registry 内部注册、watch 和 lease failure 指标。
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.meter = m
	}
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 "registry" namespace
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("registry")
		}
	}
}
