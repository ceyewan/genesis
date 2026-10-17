package cache

import (
	"context"
	"hash/fnv"
	"math"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/ceyewan/genesis/xerrors"
)

// 写操作占用全部额度，读操作各占一个额度，保留可并发读取的协调语义。
const multiExclusiveWeight int64 = math.MaxInt64

type multiCache struct {
	// 协调等待遵守请求取消，不提供跨实例失效协议。
	gates       [256]*semaphore.Weighted
	local       Local
	remote      KV
	localTTL    time.Duration
	backfillTTL time.Duration
	failOpen    bool
}

func newMulti(local Local, remote KV, cfg *MultiConfig) (Multi, error) {
	c := &multiCache{
		local:       local,
		remote:      remote,
		localTTL:    cfg.LocalTTL,
		backfillTTL: cfg.BackfillTTL,
		failOpen:    *cfg.FailOpenOnLocalError,
	}
	for n := range c.gates {
		c.gates[n] = semaphore.NewWeighted(multiExclusiveWeight)
	}
	return c, nil
}

func (c *multiCache) gateFor(key string) *semaphore.Weighted {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return c.gates[h.Sum32()%uint32(len(c.gates))]
}

func (c *multiCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if ttl < 0 {
		return ErrInvalidTTL
	}
	gate := c.gateFor(key)
	if err := gate.Acquire(ctx, multiExclusiveWeight); err != nil {
		return err
	}
	defer gate.Release(multiExclusiveWeight)
	if err := c.remote.Set(ctx, key, value, ttl); err != nil {
		return err
	}
	if err := c.local.Set(ctx, key, value, c.resolveLocalWriteTTL(ttl)); err != nil && !c.failOpen {
		return err
	}
	return nil
}

func (c *multiCache) Get(ctx context.Context, key string, dest any) error {
	gate := c.gateFor(key)
	if err := gate.Acquire(ctx, 1); err != nil {
		return err
	}
	defer gate.Release(1)
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
	gate := c.gateFor(key)
	if err := gate.Acquire(ctx, multiExclusiveWeight); err != nil {
		return err
	}
	defer gate.Release(multiExclusiveWeight)
	if err := c.remote.Delete(ctx, key); err != nil {
		return err
	}
	if err := c.local.Delete(ctx, key); err != nil && !c.failOpen {
		return err
	}
	return nil
}

func (c *multiCache) Has(ctx context.Context, key string) (bool, error) {
	gate := c.gateFor(key)
	if err := gate.Acquire(ctx, 1); err != nil {
		return false, err
	}
	defer gate.Release(1)
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
	if ttl < 0 {
		return false, ErrInvalidTTL
	}
	gate := c.gateFor(key)
	if err := gate.Acquire(ctx, multiExclusiveWeight); err != nil {
		return false, err
	}
	defer gate.Release(multiExclusiveWeight)
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
