package log

// Options 定义 log 日志器实例的配置选项
// 使用函数式选项模式，支持灵活的配置方式
type Options struct {
	// Namespace 日志器的根命名空间，通常为服务名称
	// 该命名空间会出现在此日志器实例产生的所有日志中
	Namespace string
}

// Option 定义配置 log 选项的函数类型
// 实现函数式选项模式，支持链式调用和灵活配置
type Option func(*Options)

// WithNamespace 设置日志器的命名空间
//
// 参数：
//   - namespace: 要设置的命名空间，如 "im-gateway"、"user-service" 等
//
// 返回：
//   - Option: 配置选项函数
//
// 示例：
//
//	// 创建带有服务命名空间的日志器
//	logger, err := clog.New(ctx, config, clog.WithNamespace("im-gateway"))
//
//	// 初始化全局日志器并设置命名空间
//	err := clog.Init(ctx, config, clog.WithNamespace("order-service"))
func WithNamespace(namespace string) Option {
	return func(opts *Options) {
		opts.Namespace = namespace
	}
}

// DefaultOptions 返回 clog 的默认选项
// 返回空命名空间的默认配置，作为选项解析的基础
//
// 返回：
//   - *Options: 包含默认配置的选项结构体指针
func DefaultOptions() *Options {
	return &Options{
		Namespace: "",
	}
}

// ParseOptions 解析提供的选项并返回配置好的 Options 结构体
// 将多个选项函数应用到默认配置上，生成最终的配置
//
// 参数：
//   - opts: 可变的选项函数列表
//
// 返回：
//   - *Options: 应用所有选项后的配置结构体指针
//
// 示例：
//
//	options := ParseOptions(
//		WithNamespace("payment-service"),
//		// 可以添加更多选项
//	)
func ParseOptions(opts ...Option) *Options {
	result := DefaultOptions()
	for _, opt := range opts {
		opt(result)
	}
	return result
}
