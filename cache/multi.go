package cache

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

type multiCache struct {
	local       Local
	remote      KV
	localTTL    time.Duration
	backfillTTL time.Duration
	failOpen    bool
}

func newMulti(local Local, remote KV, cfg *MultiConfig) (Multi, error) {
	return &multiCache{
		local:       local,
		remote:      remote,
		localTTL:    cfg.LocalTTL,
		backfillTTL: cfg.BackfillTTL,
		failOpen:    *cfg.FailOpenOnLocalError,
	}, nil
}

func (c *multiCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := c.remote.Set(ctx, key, value, ttl); err != nil {
		return err
	}
	if err := c.local.Set(ctx, key, value, c.resolveLocalWriteTTL(ttl)); err != nil && !c.failOpen {
		return err
	}
	return nil
}

func (c *multiCache) Get(ctx context.Context, key string, dest any) error {
	err := c.local.Get(ctx, key, dest)
	if err == nil {
		return nil
	}
	if !xerrors.Is(err, ErrMiss) && !c.failOpen {
		return err
	}

	if err := c.remote.Get(ctx, key, dest); err != nil {
		return err
	}
	if err := c.local.Set(ctx, key, dest, c.backfillTTL); err != nil && !c.failOpen {
		return err
	}
	return nil
}

func (c *multiCache) Delete(ctx context.Context, key string) error {
	if err := c.remote.Delete(ctx, key); err != nil {
		return err
	}
	if err := c.local.Delete(ctx, key); err != nil && !c.failOpen {
		return err
	}
	return nil
}

func (c *multiCache) Has(ctx context.Context, key string) (bool, error) {
	ok, err := c.local.Has(ctx, key)
	if err != nil {
		if !c.failOpen {
			return false, err
		}
	}
	if ok {
		return true, nil
	}
	return c.remote.Has(ctx, key)
}

func (c *multiCache) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := c.remote.Expire(ctx, key, ttl)
	if err != nil {
		return false, err
	}
	if !ok {
		if err := c.local.Delete(ctx, key); err != nil && !c.failOpen {
			return false, err
		}
		return false, nil
	}

	_, err = c.local.Expire(ctx, key, c.resolveLocalWriteTTL(ttl))
	if err != nil && !xerrors.Is(err, ErrMiss) {
		if c.failOpen {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

func (c *multiCache) resolveLocalWriteTTL(ttl time.Duration) time.Duration {
	if c.localTTL > 0 {
		return c.localTTL
	}
	return ttl
}

// Close 是 no-op：Multi 不拥有 local 和 remote 实例，由调用方负责关闭。
func (c *multiCache) Close() error {
	return nil
}
