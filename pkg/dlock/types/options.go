package types

import "time"

type LockOption struct {
	TTL time.Duration
}

type Option func(*LockOption)

func WithTTL(d time.Duration) Option {
	return func(o *LockOption) {
		o.TTL = d
	}
}
