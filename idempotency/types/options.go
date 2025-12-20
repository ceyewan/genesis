package types

import "time"

// DoOptions Do 方法的选项配置
type DoOptions struct {
	TTL time.Duration // 记录保留时间
}

// DoOption Do 方法的选项函数
type DoOption func(*DoOptions)

// WithTTL 设置记录保留时间
func WithTTL(ttl time.Duration) DoOption {
	return func(o *DoOptions) {
		o.TTL = ttl
	}
}

