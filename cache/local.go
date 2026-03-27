package cache

import (
	"context"
	"time"

	"github.com/maypok86/otter/v2"
	"github.com/maypok86/otter/v2/stats"

	"github.com/ceyewan/genesis/cache/serializer"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// localEntry 包装缓存数据和 TTL，供 ExpiryWritingFunc 使用。
type localEntry struct {
	data []byte
	ttl  time.Duration
}

type localCache struct {
	cache      *otter.Cache[string, localEntry]
	serializer serializer.Serializer
	defaultTTL time.Duration
	logger     clog.Logger
	meter      metrics.Meter
}

func newLocal(cfg *LocalConfig, logger clog.Logger, meter metrics.Meter) (Local, error) {
	if cfg == nil {
		return nil, xerrors.New("cache: local config is nil")
	}

	s, err := serializer.New(cfg.Serializer)
	if err != nil {
		return nil, err
	}

	// ExpiryWritingFunc 在每次写入时从 entry 中读取 TTL，保证 Set 和 TTL 原子更新。
	cache, err := otter.New(&otter.Options[string, localEntry]{
		MaximumSize:   cfg.MaxEntries,
		StatsRecorder: stats.NewCounter(),
		ExpiryCalculator: otter.ExpiryWritingFunc(func(entry otter.Entry[string, localEntry]) time.Duration {
			return entry.Value.ttl
		}),
	})
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to build otter cache")
	}

	return &localCache{
		cache:      cache,
		serializer: s,
		defaultTTL: cfg.DefaultTTL,
		logger:     logger,
		meter:      meter,
	}, nil
}

func (c *localCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := c.serializer.Marshal(value)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	// 单次 Set 同时写入数据与 TTL，避免两步操作之间的竞态。
	c.cache.Set(key, localEntry{data: data, ttl: ttl})
	return nil
}

func (c *localCache) Get(ctx context.Context, key string, dest any) error {
	entry, ok := c.cache.GetIfPresent(key)
	if !ok {
		return ErrMiss
	}
	return c.serializer.Unmarshal(entry.data, dest)
}

func (c *localCache) Delete(ctx context.Context, key string) error {
	c.cache.Invalidate(key)
	return nil
}

func (c *localCache) Has(ctx context.Context, key string) (bool, error) {
	_, ok := c.cache.GetIfPresent(key)
	return ok, nil
}

func (c *localCache) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	existing, ok := c.cache.GetIfPresent(key)
	if !ok {
		return false, nil
	}
	c.cache.Set(key, localEntry{data: existing.data, ttl: ttl})
	return true, nil
}

func (c *localCache) Close() error {
	c.cache.StopAllGoroutines()
	return nil
}
