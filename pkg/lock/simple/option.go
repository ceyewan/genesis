package simple

import "time"

// Option 锁行为配置（可选参数）
type Option struct {
	TTL           time.Duration // 锁超时时间（默认10s）
	RetryInterval time.Duration // 重试间隔（默认100ms）
	AutoRenew     bool          // 自动续期（默认true）
	MaxRetries    int           // 最大重试次数（默认0，表示无限重试）
}

// DefaultOption 默认行为配置
func DefaultOption() *Option {
	return &Option{
		TTL:           10 * time.Second,
		RetryInterval: 100 * time.Millisecond,
		AutoRenew:     true,
		MaxRetries:    0,
	}
}
