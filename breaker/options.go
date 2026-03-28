package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"
)

// Option 组件初始化选项函数
type Option func(*options)

// FallbackFunc 拒绝处理函数类型。
// 当熔断器拒绝执行请求时，可以执行自定义处理逻辑。
// 参数:
//   - ctx: 上下文
//   - key: 熔断键
//   - err: 原始拒绝错误（ErrOpenState 或 ErrTooManyRequests）
//
// 返回:
//   - error: 自定义处理结果；返回 nil 表示吞掉本次拒绝
type FallbackFunc func(ctx context.Context, key string, err error) error

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger   clog.Logger
	fallback FallbackFunc
}

// WithLogger 设置 Logger，传入 nil 时使用 clog.Discard()
// 内部会自动添加 namespace: "breaker"
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger == nil {
			o.logger = clog.Discard()
		} else {
			o.logger = logger.WithNamespace("breaker")
		}
	}
}

// WithFallback 设置拒绝处理函数。
// 当熔断器因打开状态或半开探测上限而拒绝请求时，会调用此函数。
//
// 使用示例:
//
//	brk, _ := breaker.New(cfg,
//		breaker.WithFallback(func(ctx context.Context, key string, err error) error {
//			logger.Info("Circuit breaker rejected request", clog.String("key", key))
//			return nil
//		}),
//	)
func WithFallback(fallback FallbackFunc) Option {
	return func(o *options) {
		o.fallback = fallback
	}
}
