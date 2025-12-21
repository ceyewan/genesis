package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*options)

// FallbackFunc 降级函数类型
// 当熔断器打开时，可以执行自定义的降级逻辑
// 参数:
//   - ctx: 上下文
//   - serviceName: 服务名
//   - err: 原始错误（通常是 ErrOpenState）
// 返回:
//   - error: 降级逻辑的错误，nil 表示降级成功
type FallbackFunc func(ctx context.Context, serviceName string, err error) error

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger   clog.Logger
	meter    metrics.Meter
	fallback FallbackFunc
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithMeter 设置 Meter
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		o.meter = meter
	}
}

// WithFallback 设置降级函数
// 当熔断器打开时，会调用此函数进行降级处理
//
// 使用示例:
//
//	brk, _ := breaker.New(cfg,
//		breaker.WithFallback(func(ctx context.Context, serviceName string, err error) error {
//			// 返回缓存数据或默认值
//			logger.Warn("circuit breaker open, using fallback", clog.String("service", serviceName))
//			return nil
//		}),
//	)
func WithFallback(fallback FallbackFunc) Option {
	return func(o *options) {
		o.fallback = fallback
	}
}

