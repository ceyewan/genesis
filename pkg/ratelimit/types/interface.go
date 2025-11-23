package types

import (
	"context"
	"errors"
)

// Limit 定义限流规则 (令牌桶算法)
type Limit struct {
	Rate  float64 // 令牌生成速率 (每秒生成多少个令牌)
	Burst int     // 令牌桶容量 (突发最大请求数)
}

// Limiter 限流器核心接口
type Limiter interface {
	// Allow 尝试获取 1 个令牌 (非阻塞)
	// key: 限流标识 (如 IP, UserID, ServiceName)
	// limit: 限流规则
	// 返回: allowed (是否允许), error (系统错误)
	Allow(ctx context.Context, key string, limit Limit) (bool, error)

	// AllowN 尝试获取 N 个令牌 (非阻塞)
	// key: 限流标识
	// limit: 限流规则
	// n: 请求的令牌数量
	// 返回: allowed (是否允许), error (系统错误)
	AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error)

	// Wait 阻塞等待直到获取 1 个令牌
	// 注意：分布式实现可能不支持此方法或性能较低
	// key: 限流标识
	// limit: 限流规则
	// 返回: error (系统错误或超时)
	Wait(ctx context.Context, key string, limit Limit) error
}

// 错误定义
var (
	// ErrNotSupported 表示操作不支持
	ErrNotSupported = errors.New("operation not supported")

	// ErrKeyEmpty 表示限流键为空
	ErrKeyEmpty = errors.New("rate limit key is empty")

	// ErrInvalidLimit 表示限流规则无效
	ErrInvalidLimit = errors.New("invalid rate limit")
)

