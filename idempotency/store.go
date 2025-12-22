package idempotency

import (
	"context"
	"time"
)

// ========================================
// 存储接口 (Store Interface)
// ========================================

// Store 幂等性存储接口
//
// 定义了幂等性组件与存储后端的交互方式。
// 存储后端需要支持三种状态：
//  1. 锁定中（processing）: Lock() 成功后的状态
//  2. 已完成（completed）: SetResult() 后的状态
//  3. 不存在（absent）: 初始状态或 TTL 过期后
//
// 默认提供 Redis 实现，用户可以实现此接口以支持其他存储后端。
type Store interface {
	// Lock 尝试获取锁（标记处理中）
	// 返回 true 表示成功获取锁，false 表示已被其他请求锁定
	Lock(ctx context.Context, key string, ttl time.Duration) (bool, error)

	// Unlock 释放锁（通常用于执行失败时清理）
	Unlock(ctx context.Context, key string) error

	// SetResult 保存执行结果并标记完成
	// 同时会自动释放锁
	SetResult(ctx context.Context, key string, val []byte, ttl time.Duration) error

	// GetResult 获取已完成的结果
	// 如果结果不存在，返回 ErrResultNotFound
	GetResult(ctx context.Context, key string) ([]byte, error)

	// WaitForResult 等待结果完成（用于并发请求）
	// 如果在 timeout 内结果完成，返回结果
	// 如果超时，返回 ErrWaitTimeout
	WaitForResult(ctx context.Context, key string, timeout time.Duration) ([]byte, error)
}

// ========================================
// 存储状态常量
// ========================================

const (
	// lockSuffix 锁的 Redis key 后缀
	lockSuffix = ":lock"

	// resultSuffix 结果的 Redis key 后缀
	resultSuffix = ":result"
)
