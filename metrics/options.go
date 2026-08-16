package metrics

import "github.com/ceyewan/genesis/clog"

// Option 配置 Meter 实例。
type Option func(*options)

type options struct {
	logger clog.Logger
}

func applyOptions(opts ...Option) *options {
	o := &options{logger: clog.Discard()}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	return o
}

// WithLogger 注入 metrics 内部生命周期日志使用的 Logger。
//
// metrics 不会关闭注入的 Logger；资源所有权仍属于调用方。
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger == nil {
			o.logger = clog.Discard()
			return
		}
		o.logger = logger.WithNamespace("metrics")
	}
}
