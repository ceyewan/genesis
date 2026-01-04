package auth

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 配置选项函数
type Option func(*options)

// options 内部选项结构
type options struct {
	logger clog.Logger
	meter  metrics.Meter
}

// defaultOptions 创建默认选项，使用 Discard() 作为空实现
func defaultOptions() *options {
	return &options{
		logger: clog.Discard(),
		meter:  metrics.Discard(),
	}
}

// WithLogger 注入日志记录器，自动添加 "auth" 命名空间
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("auth")
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
