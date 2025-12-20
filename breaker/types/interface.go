package types

import (
	"context"
	"errors"
)

// State 熔断器状态
type State string

const (
	StateClosed   State = "closed"    // 正常状态，请求正常通过
	StateOpen     State = "open"      // 熔断状态，请求快速失败
	StateHalfOpen State = "half_open" // 半开状态，允许少量探测请求
)

// String 返回状态的字符串表示
func (s State) String() string {
	return string(s)
}

// Breaker 熔断器核心接口
type Breaker interface {
	// Execute 执行受保护的函数
	// key: 服务标识（如 "user.v1.UserService"）
	// fn: 实际业务逻辑
	// 返回: 业务逻辑的 error，或熔断器错误 ErrOpenState
	Execute(ctx context.Context, key string, fn func() error) error

	// ExecuteWithFallback 执行受保护的函数，并提供降级逻辑
	// key: 服务标识
	// fn: 实际业务逻辑
	// fallback: 熔断时的降级函数，接收原始错误，返回降级结果的错误
	ExecuteWithFallback(ctx context.Context, key string, fn func() error, fallback func(error) error) error

	// State 获取指定服务的熔断器状态
	State(key string) State

	// Reset 手动重置指定服务的熔断器状态为 Closed
	// 用于运维场景的强制恢复
	Reset(key string)
}

// 错误定义
var (
	// ErrOpenState 熔断器处于 Open 状态，请求被拒绝
	ErrOpenState = errors.New("circuit breaker is open")

	// ErrTooManyRequests 半开状态下探测请求数已达上限
	ErrTooManyRequests = errors.New("too many requests in half-open state")

	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = errors.New("invalid breaker configuration")
)
