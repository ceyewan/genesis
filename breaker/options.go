package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
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
// 返回替代结果和错误，其语义与 Execute 的返回值一致。
type FallbackFunc func(ctx context.Context, key string, err error) (any, error)

// FailureClassifier 决定 Execute 返回的错误是否计入熔断失败。
// 返回 true 表示计为失败；默认所有非 nil error 都计为失败。
type FailureClassifier func(error) bool

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger            clog.Logger
	meter             metrics.Meter
	fallback          FallbackFunc
	failureClassifier FailureClassifier
}

// WithMeter 注入 breaker 的执行、拒绝和状态指标。
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		o.meter = meter
	}
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
//		breaker.WithFallback(func(ctx context.Context, key string, err error) (any, error) {
//			logger.Info("Circuit breaker rejected request", clog.String("key", key))
//			return cachedValue, nil
//		}),
//	)
func WithFallback(fallback FallbackFunc) Option {
	return func(o *options) {
		o.fallback = fallback
	}
}

// WithFailureClassifier 自定义通用 Execute 的失败口径。
// gRPC interceptor 仍会先应用内置的 gRPC 状态码分类。
func WithFailureClassifier(classifier FailureClassifier) Option {
	return func(o *options) {
		o.failureClassifier = classifier
	}
}
