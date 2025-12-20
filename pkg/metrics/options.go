package metrics

// Option 用于配置 Meter 的选项
type Option func(*options)

// options 内部选项结构
type options struct {
	logger interface{}
}

// WithLogger 设置 Logger
func WithLogger(logger interface{}) Option {
	return func(o *options) {
		o.logger = logger
	}
}
