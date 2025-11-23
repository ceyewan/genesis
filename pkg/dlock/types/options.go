package types

import "time"

// LockOptions Lock 操作的选项配置
// 用于 Lock() 和 TryLock() 方法的运行时参数
type LockOptions struct {
	TTL time.Duration
}

// LockOption Lock 操作的选项函数
type LockOption func(*LockOptions)

// WithTTL 设置锁的 TTL（超时时间）
// 用于覆盖配置中的 DefaultTTL
//
// 使用示例:
//
//	locker.Lock(ctx, "key", dlock.WithTTL(10*time.Second))
func WithTTL(d time.Duration) LockOption {
	return func(o *LockOptions) {
		o.TTL = d
	}
}
