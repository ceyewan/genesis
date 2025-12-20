package types

import (
	"context"
)

// Locker 定义了分布式锁的核心行为
type Locker interface {
	// Lock 阻塞式加锁
	// 成功返回 nil，失败返回错误
	// 如果上下文取消，返回 context.Canceled 或 context.DeadlineExceeded
	//
	// opts 支持的选项:
	//   - WithTTL(duration): 设置锁的超时时间
	Lock(ctx context.Context, key string, opts ...LockOption) error

	// TryLock 非阻塞式尝试加锁
	// 成功获取锁返回 true, nil
	// 锁已被占用返回 false, nil
	// 发生错误返回 false, err
	//
	// opts 支持的选项:
	//   - WithTTL(duration): 设置锁的超时时间
	TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error)

	// Unlock 释放锁
	// 只有锁的持有者才能成功释放
	Unlock(ctx context.Context, key string) error
}
