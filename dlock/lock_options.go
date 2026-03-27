package dlock

import "time"

// lockOptions Lock 操作的选项配置
// 用于 Lock() 和 TryLock() 方法的运行时参数
type lockOptions struct {
	TTL    time.Duration
	ttlSet bool
}

// LockOption Lock 操作的选项函数
type LockOption func(*lockOptions)

// WithTTL 设置锁的 TTL（超时时间）
// 用于覆盖配置中的 DefaultTTL。
//
// 需要注意：
//
//   - Redis 支持原生 time.Duration 精度
//   - Etcd 的 TTL 基于 lease，必须至少 1 秒且为整秒，否则返回 ErrInvalidTTL
//
// 使用示例:
//
//	locker.Lock(ctx, "key", dlock.WithTTL(10*time.Second))
func WithTTL(d time.Duration) LockOption {
	return func(o *lockOptions) {
		o.TTL = d
		o.ttlSet = true
	}
}
