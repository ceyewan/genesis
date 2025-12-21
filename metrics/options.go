package metrics

import "github.com/ceyewan/genesis/clog"

// Option 配置 Meter 实例的选项函数类型
// 用于在创建 Meter 实例时注入自定义配置
type Option func(*options)

// options 内部选项结构，存储 Meter 的配置信息
// 这个结构体是非导出的，只能通过 Option 函数进行修改
type options struct {
	// Logger 日志记录器，用于记录指标系统的内部事件
	// 如果未设置，将使用 slog.Default() 作为默认日志器
	logger clog.Logger
}

// WithLogger 注入日志记录器
// 用于自定义指标系统的日志输出，例如调试信息、错误日志等
// 组件会自动为 logger 添加 "metrics" 命名空间
//
// 参数：
//
//	logger - clog.Logger 实例，如果为 nil 则忽略
//
// 使用示例：
//
//	logger := clog.MustLoad(&clog.Config{Level: "info"})
//	meter, err := metrics.New(cfg, metrics.WithLogger(logger))
//
// 返回：
//
//	Option 函数，可用于 metrics.New() 的参数
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			// 自动添加 metrics 命名空间，保持日志的可追踪性
			o.logger = logger.WithNamespace("metrics")
		}
	}
}
