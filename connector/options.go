package connector

import "github.com/ceyewan/genesis/clog"

type options struct {
	logger clog.Logger
}

// Option 配置连接器的选项
type Option func(*options)

// applyDefaults 确保未设置的选项使用默认值
func (o *options) applyDefaults() {
	if o.logger == nil {
		o.logger = clog.Discard()
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
