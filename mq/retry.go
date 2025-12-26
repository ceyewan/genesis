package mq

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
)

// HandlerMiddleware 定义消息处理中间件
type HandlerMiddleware func(Handler) Handler

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries     int           // 最大重试次数
	InitialBackoff time.Duration // 初始退避时间
	MaxBackoff     time.Duration // 最大退避时间
	Multiplier     float64       // 退避倍数 (默认 2.0)
}

// DefaultRetryConfig 默认重试配置
var DefaultRetryConfig = RetryConfig{
	MaxRetries:     3,
	InitialBackoff: 100 * time.Millisecond,
	MaxBackoff:     1 * time.Second,
	Multiplier:     2.0,
}

// WithRetry 返回一个带有指数退避重试逻辑的中间件
func WithRetry(cfg RetryConfig, logger clog.Logger) HandlerMiddleware {
	if cfg.Multiplier <= 1.0 {
		cfg.Multiplier = 2.0
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, msg Message) error {
			var err error
			backoff := cfg.InitialBackoff

			for i := 0; i <= cfg.MaxRetries; i++ {
				// 尝试执行处理
				if err = next(ctx, msg); err == nil {
					return nil
				}

				// 如果是最后一次尝试，直接返回错误
				if i == cfg.MaxRetries {
					break
				}

				// 记录重试日志
				if logger != nil {
					logger.Warn("消息处理失败，准备重试",
						clog.String("subject", msg.Subject()),
						clog.Int("retry_count", i+1),
						clog.Duration("backoff", backoff),
						clog.Error(err),
					)
				}

				// 等待退避时间
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(backoff):
				}

				// 计算下一次退避时间
				backoff = time.Duration(float64(backoff) * cfg.Multiplier)
				if backoff > cfg.MaxBackoff {
					backoff = cfg.MaxBackoff
				}
			}
			return err
		}
	}
}
