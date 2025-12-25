package metrics

import "github.com/ceyewan/genesis/clog"

// Option 配置 Meter 实例的选项
type Option func(*options)

// options 内部选项结构
type options struct {
	logger clog.Logger
}

// WithLogger 注入日志记录器
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger.WithNamespace("metrics")
		}
	}
}
