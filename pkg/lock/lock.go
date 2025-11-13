// pkg/lock/lock.go
package lock

import (
	"context"
	"time"
)

// Locker 分布式锁接口
// 设计原则：简单、通用，支持不同底层实现（etcd、redis等）
type Locker interface {
	// Lock 阻塞式加锁，直到成功获取锁或上下文取消
	Lock(ctx context.Context, key string) error

	// TryLock 非阻塞式加锁，立即返回是否成功
	TryLock(ctx context.Context, key string) (bool, error)

	// Unlock 释放锁
	Unlock(ctx context.Context, key string) error

	// LockWithTTL 带TTL的加锁，自动续期
	// ttl: 锁的超时时间
	LockWithTTL(ctx context.Context, key string, ttl time.Duration) error

	// Close 关闭锁客户端，释放资源
	Close() error
}

// LockOptions 锁配置选项
type LockOptions struct {
	// TTL 锁的默认超时时间
	TTL time.Duration
	// RetryInterval 重试间隔
	RetryInterval time.Duration
	// AutoRenew 是否自动续期
	AutoRenew bool
}

// DefaultLockOptions 默认配置
func DefaultLockOptions() *LockOptions {
	return &LockOptions{
		TTL:           10 * time.Second,
		RetryInterval: 100 * time.Millisecond,
		AutoRenew:     true,
	}
}
