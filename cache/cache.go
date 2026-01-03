// Package cache 提供缓存组件，支持基于 Redis 的多种数据结构操作。
//
// Cache 组件是 Genesis 微服务组件库的缓存抽象层，提供了统一的缓存操作语义。
// 支持 Redis 的核心数据结构：String、Hash、Sorted Set、List，并支持自动序列化。
//
// 基本使用：
//
//	redisConn, _ := connector.NewRedis(redisConfig)
//	cacheClient, _ := cache.New(redisConn, &cache.Config{
//	    Prefix:     "myapp:",
//	    Serializer: "json",
//	}, cache.WithLogger(logger))
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
	"github.com/ceyewan/genesis/connector"
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

// New 根据配置创建缓存实例
//
// 支持单机模式 (standalone) 和分布式模式 (distributed)。
// 如果 Mode 为 "standalone"，则创建本地内存缓存；
// 如果 Mode 为 "distributed" 或为空，则创建 Redis 缓存。
//
// 注意：分布式模式需要通过 WithRedisConnector 注入 Redis 连接器，
// 或者使用兼容旧版的 NewWithRedis 函数。
func New(cfg *Config, opts ...Option) (Cache, error) {
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 注入默认日志器
	if opt.Logger == nil {
		var err error
		opt.Logger, err = clog.New(&clog.Config{Level: "info"})
		if err != nil {
			return nil, xerrors.New("failed to create default logger: " + err.Error())
		}
	}

	switch cfg.Mode {
	case "standalone":
		return NewStandalone(cfg.Standalone, opts...)
	case "distributed", "":
		if opt.RedisConn == nil {
			return nil, xerrors.New("redis connector is required for distributed mode, use WithRedisConnector")
		}
		return NewWithRedis(opt.RedisConn, cfg, opts...)
	default:
		return NewWithRedis(opt.RedisConn, cfg, opts...)
	}
}

// NewStandalone 创建单机内存缓存实例
//
// 参数:
//   - cfg: 单机缓存配置
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	cacheClient, _ := cache.NewStandalone(&cache.StandaloneConfig{
//	    Capacity: 10000,
//	}, cache.WithLogger(logger))
func NewStandalone(cfg *StandaloneConfig, opts ...Option) (Cache, error) {
	if cfg == nil {
		cfg = &StandaloneConfig{
			Capacity: 10000,
		}
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	if opt.Logger == nil {
		var err error
		opt.Logger, err = clog.New(&clog.Config{Level: "info"})
		if err != nil {
			return nil, xerrors.New("failed to create default logger: " + err.Error())
		}
	}

	return newStandalone(cfg, opt.Logger, opt.Meter)
}

// NewWithRedis 创建 Redis 缓存实例 (兼容旧版)
//
// 参数:
//   - conn: Redis 连接器
//   - cfg: 缓存配置
//   - opts: 可选参数 (Logger, Meter)
func NewWithRedis(conn connector.RedisConnector, cfg *Config, opts ...Option) (Cache, error) {
	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 如果没有提供 Logger，创建默认实例
	if opt.Logger == nil {
		var err error
		opt.Logger, err = clog.New(&clog.Config{Level: "info"})
		if err != nil {
			return nil, xerrors.New("failed to create default logger: " + err.Error())
		}
	}

	return newRedis(conn, cfg, opt.Logger, opt.Meter)
}
