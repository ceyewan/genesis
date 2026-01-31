package registry

import "github.com/ceyewan/genesis/clog"

// Option 组件初始化选项函数
type Option func(*options)

// options 选项结构
type options struct {
	logger clog.Logger
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
