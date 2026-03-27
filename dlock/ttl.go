package dlock

import (
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

func resolveLockTTL(defaultTTL time.Duration, opts ...LockOption) (time.Duration, error) {
	options := &lockOptions{}
	for _, opt := range opts {
		opt(options)
	}

	if !options.ttlSet {
		return defaultTTL, nil
	}
	if options.TTL <= 0 {
		return 0, xerrors.Wrap(ErrInvalidTTL, "ttl must be greater than 0")
	}
	return options.TTL, nil
}

func validateEtcdTTL(ttl time.Duration) error {
	if ttl < time.Second {
		return xerrors.Wrap(ErrInvalidTTL, "etcd ttl must be at least 1s")
	}
	if ttl%time.Second != 0 {
		return xerrors.Wrap(ErrInvalidTTL, "etcd ttl must be a whole number of seconds")
	}
	return nil
}
