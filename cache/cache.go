// Package cache 提供缓存组件，支持基于 Redis 的多种数据结构操作。
//
// Cache 组件是 Genesis 微服务组件库的缓存抽象层，提供了统一的缓存操作语义。
// 支持 Redis 的核心数据结构：String、Hash、Sorted Set、List，并支持自动序列化。
//
// 基本使用：
//
//	redisConn, _ := connector.NewRedis(redisConfig)
//	cacheClient, _ := cache.New(&cache.Config{
//	    Driver:     cache.DriverRedis,
//	    Prefix:     "myapp:",
//	    Serializer: "json",
//	}, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))
//
//	// 缓存对象
//	err := cacheClient.Set(ctx, "user:1001", user, time.Hour)
//
//	// 获取对象
//	var cachedUser User
//	err = cacheClient.Get(ctx, "user:1001", &cachedUser)
//
//	// Hash 操作
//	err = cacheClient.HSet(ctx, "user:1001:profile", "name", "Alice")
//	err = cacheClient.HGet(ctx, "user:1001:profile", "name", &name)
package cache

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// Cache 定义了缓存组件的核心能力
type Cache interface {
	// --- Key-Value ---
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Get(ctx context.Context, key string, dest any) error
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// --- Hash(Distributed Only) ---
	HSet(ctx context.Context, key string, field string, value any) error
	HGet(ctx context.Context, key string, field string, dest any) error
	HGetAll(ctx context.Context, key string, destMap any) error
	HDel(ctx context.Context, key string, fields ...string) error
	HIncrBy(ctx context.Context, key string, field string, increment int64) (int64, error)

	// --- Sorted Set(Distributed Only) ---
	ZAdd(ctx context.Context, key string, score float64, member any) error
	ZRem(ctx context.Context, key string, members ...any) error
	ZScore(ctx context.Context, key string, member any) (float64, error)
	ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error

	// --- List(Distributed Only) ---
	LPush(ctx context.Context, key string, values ...any) error
	RPush(ctx context.Context, key string, values ...any) error
	LPop(ctx context.Context, key string, dest any) error
	RPop(ctx context.Context, key string, dest any) error
	LRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	LPushCapped(ctx context.Context, key string, limit int64, values ...any) error

	// --- Utility ---
	Close() error
}

// New 根据配置创建缓存实例（配置驱动）
//
// 通过 cfg.Driver 选择后端，连接器通过 Option 注入：
//   - DriverRedis: WithRedisConnector
//   - DriverMemory: 无需连接器
func New(cfg *Config, opts ...Option) (Cache, error) {
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	if opt.Logger == nil {
		opt.Logger = clog.Discard()
	}

	if opt.Meter == nil {
		opt.Meter = metrics.Discard()
	}

	switch cfg.Driver {
	case DriverMemory:
		return newStandalone(cfg.Standalone, opt.Logger, opt.Meter)
	case DriverRedis:
		if opt.RedisConn == nil {
			return nil, xerrors.New("redis connector is required, use WithRedisConnector")
		}
		return newRedis(opt.RedisConn, cfg, opt.Logger, opt.Meter)
	default:
		return nil, xerrors.New("unsupported driver: " + string(cfg.Driver))
	}
}
