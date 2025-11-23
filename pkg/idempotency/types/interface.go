package types

import (
	"context"
	"errors"
)

// Status 幂等记录的状态
type Status int

const (
	StatusProcessing Status = iota // 处理中 (锁定)
	StatusSuccess                  // 处理成功
	StatusFailed                   // 处理失败
)

// String 返回状态的字符串表示
func (s Status) String() string {
	switch s {
	case StatusProcessing:
		return "processing"
	case StatusSuccess:
		return "success"
	case StatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Idempotent 幂等组件对外接口
type Idempotent interface {
	// Do 执行幂等操作
	// key: 幂等键
	// fn: 实际业务逻辑，返回 result 和 error
	// opts: 选项 (如 TTL)
	// 返回: 业务逻辑的结果和错误
	Do(ctx context.Context, key string, fn func() (any, error), opts ...DoOption) (any, error)

	// Check 检查幂等键的状态
	// key: 幂等键
	// 返回: 状态、结果（如果有）、错误
	Check(ctx context.Context, key string) (Status, any, error)

	// Delete 删除幂等记录（用于手动清理或重试）
	// key: 幂等键
	Delete(ctx context.Context, key string) error
}

// 错误定义
var (
	// ErrProcessing 表示请求正在处理中
	ErrProcessing = errors.New("request is being processed")

	// ErrKeyEmpty 表示幂等键为空
	ErrKeyEmpty = errors.New("idempotency key is empty")

	// ErrNotFound 表示幂等记录不存在
	ErrNotFound = errors.New("idempotency record not found")
)
