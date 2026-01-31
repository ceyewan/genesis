package mqv2

import (
	"time"

	"github.com/ceyewan/genesis/clog"
)

// Middleware Handler 中间件
//
// 中间件模式允许在不修改业务逻辑的情况下增强 Handler 能力。
// 常见用途：重试、日志、链路追踪、指标等。
type Middleware func(Handler) Handler

// Chain 将多个中间件串联成一个
//
// 执行顺序：第一个中间件最先执行，最后一个最接近原始 Handler。
//
// 示例：
//
//	handler = mqv2.Chain(logging, retry, tracing)(handler)
//	// 执行顺序：logging -> retry -> tracing -> handler
func Chain(middlewares ...Middleware) Middleware {
	return func(next Handler) Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

// RetryConfig 重试配置
type RetryConfig struct {
	// MaxRetries 最大重试次数（不含首次执行）
	MaxRetries int

	// InitialBackoff 初始退避时间
	InitialBackoff time.Duration

	// MaxBackoff 最大退避时间
	MaxBackoff time.Duration

	// Multiplier 退避倍数
	Multiplier float64
}

// DefaultRetryConfig 默认重试配置
var DefaultRetryConfig = RetryConfig{
	MaxRetries:     3,
	InitialBackoff: 100 * time.Millisecond,
	MaxBackoff:     5 * time.Second,
	Multiplier:     2.0,
}

// WithRetry 创建重试中间件
//
// 在应用层实现重试逻辑（不依赖 MQ 后端的重投机制）。
// 适用于幂等操作或可安全重试的场景。
//
// 注意：
//   - 重试发生在单次消息处理内，不影响 MQ 层面的 Ack/Nak
//   - 如果所有重试都失败，最终错误会返回给上层（可能触发 Nak）
//
// 示例：
//
//	handler := mqv2.WithRetry(mqv2.DefaultRetryConfig, logger)(myHandler)
func WithRetry(cfg RetryConfig, logger clog.Logger) Middleware {
	if cfg.Multiplier <= 1.0 {
		cfg.Multiplier = 2.0
	}

	return func(next Handler) Handler {
		return func(msg Message) error {
			var err error
			backoff := cfg.InitialBackoff

			for attempt := 0; attempt <= cfg.MaxRetries; attempt++ {
				err = next(msg)
				if err == nil {
					return nil
				}

				// 最后一次尝试，直接返回错误
				if attempt == cfg.MaxRetries {
					break
				}

				// 记录重试日志
				if logger != nil {
					logger.Warn("message handler failed, retrying",
						clog.String("topic", msg.Topic()),
						clog.String("msg_id", msg.ID()),
						clog.Int("attempt", attempt+1),
						clog.Int("max_retries", cfg.MaxRetries),
						clog.Duration("backoff", backoff),
						clog.Error(err),
					)
				}

				// 检查 context 是否已取消
				select {
				case <-msg.Context().Done():
					return msg.Context().Err()
				case <-time.After(backoff):
				}

				// 计算下次退避时间
				backoff = time.Duration(float64(backoff) * cfg.Multiplier)
				if backoff > cfg.MaxBackoff {
					backoff = cfg.MaxBackoff
				}
			}

			return err
		}
	}
}

// WithLogging 创建日志中间件
//
// 记录每条消息的处理情况，包括：topic、消息 ID、处理耗时、错误信息。
func WithLogging(logger clog.Logger) Middleware {
	return func(next Handler) Handler {
		return func(msg Message) error {
			start := time.Now()
			err := next(msg)
			duration := time.Since(start)

			if err != nil {
				logger.Error("message handler failed",
					clog.String("topic", msg.Topic()),
					clog.String("msg_id", msg.ID()),
					clog.Duration("duration", duration),
					clog.Error(err),
				)
			} else {
				logger.Debug("message handled",
					clog.String("topic", msg.Topic()),
					clog.String("msg_id", msg.ID()),
					clog.Duration("duration", duration),
				)
			}

			return err
		}
	}
}

// WithRecover 创建 panic 恢复中间件
//
// 捕获 Handler 中的 panic，转换为错误返回，避免整个消费者崩溃。
func WithRecover(logger clog.Logger) Middleware {
	return func(next Handler) Handler {
		return func(msg Message) (err error) {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("message handler panic recovered",
						clog.String("topic", msg.Topic()),
						clog.String("msg_id", msg.ID()),
						clog.Any("panic", r),
					)
					err = ErrPanicRecovered
				}
			}()
			return next(msg)
		}
	}
}
