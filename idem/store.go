package idem

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
// 默认提供 Redis / Memory 实现。
type Store interface {
	// Lock 尝试获取锁（标记处理中）。实现应尽可能原子地确认没有已完成
	// 结果再创建锁；返回 true 表示成功获取锁，false 表示已有结果或已被
	// 其他请求锁定。Idempotency 会在 Lock 后再次读取结果，以兼容无法提供
	// 该原子检查的第三方 Store。实现若限制容量，成功 Lock 必须同时为该
	// token 后续的 SetResult 预留提交容量，不能等业务执行完成后才以
	// ErrStoreCapacity 拒绝 lock 到 result 的状态转换。
	Lock(ctx context.Context, key string, ttl time.Duration) (LockToken, bool, error)

	// Unlock 释放锁（通常用于执行失败时清理）
	Unlock(ctx context.Context, key string, token LockToken) error

	// SetResult 保存执行结果并标记完成，同时原子释放匹配 token 的锁。
	// 对成功 Lock 返回的 token，这个状态转换不得因容量不足而失败。
	SetResult(ctx context.Context, key string, val []byte, ttl time.Duration, token LockToken) error

	// GetResult 获取已完成的结果
	// 如果结果不存在，返回 ErrResultNotFound
	GetResult(ctx context.Context, key string) ([]byte, error)
}

// RefreshableStore 可刷新锁 TTL 的存储实现
// 用于长时间执行时保持锁不失效
type RefreshableStore interface {
	Store
	Refresh(ctx context.Context, key string, token LockToken, ttl time.Duration) error
}

// DeletableStore 可删除缓存结果的存储实现。
// 用于清理损坏的缓存数据并触发重新执行。
type DeletableStore interface {
	Store
	DeleteResult(ctx context.Context, key string) error
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

// LockToken 锁令牌，用于保证解锁安全
type LockToken string
