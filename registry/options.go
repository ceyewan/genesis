package registry

import (
	"github.com/ceyewan/genesis/clog"
	metrics "github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 选项结构（导出供 internal 使用）
type options struct {
	logger clog.Logger
	meter  metrics.Meter
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

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.meter = m
	}
}

// defaultOptions 返回默认选项
func defaultOptions() *options {
	logger, _ := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
		Output: "stdout",
	})
	return &options{
		logger: logger,
	}
}
