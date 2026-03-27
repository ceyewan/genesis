// Package cache 提供 Genesis 的缓存组件族，支持分布式缓存、本地缓存和多级缓存。
//
// 组件分类：
//   - Distributed: 基于 Redis 的分布式缓存，支持 KV / Hash / ZSet / Batch。
//   - Local: 基于进程内存的本地缓存，提供稳定的 KV 语义。
//   - Multi: 组合 Local + Distributed 的两级缓存。
//
// 语义约定：
//   - Get 等读取操作未命中时返回 ErrMiss。
//   - Has 不返回 ErrMiss，而是通过 bool 表达存在性。
//   - Set 和 Expire 在 ttl<=0 时使用组件配置中的 DefaultTTL。
//   - Local 与 Multi 仅提供 KV 能力；Hash、ZSet、Batch 仅由 Distributed 提供。
//   - RawClient 用于 Pipeline、Lua 脚本等高级场景，不保证跨后端兼容。
package cache

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// KV 定义缓存组件的稳定 KV 能力。
//
// 这是 Local、Distributed 和 Multi 共享的最小公共语义。调用方可以依赖如下约定：
//   - Set 在 ttl>0 时使用显式 TTL，在 ttl<=0 时使用组件的 DefaultTTL。
//   - Get 未命中时返回 ErrMiss。
//   - Delete 删除不存在的 key 不视为错误。
//   - Expire 返回值中的 bool 表示 key 是否存在。
type KV interface {
	// Set 设置缓存值。
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	// Get 读取缓存值；未命中时返回 ErrMiss。
	Get(ctx context.Context, key string, dest any) error
	// Delete 删除缓存值。
	Delete(ctx context.Context, key string) error
	// Has 判断 key 是否存在。
	Has(ctx context.Context, key string) (bool, error)
	// Expire 更新 key 的 TTL；ttl<=0 时使用组件配置的 DefaultTTL；bool=false 表示 key 不存在。
	Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Close 释放缓存实例拥有的资源。
	Close() error
}

// Distributed 定义分布式缓存能力。
//
// 当前唯一实现基于 Redis。除 KV 语义外，Distributed 还提供 Hash、Sorted Set、Batch 和 RawClient 等 Redis 导向能力。
type Distributed interface {
	KV
	// HSet 设置 Hash 字段。
	HSet(ctx context.Context, key string, field string, value any) error
	// HGet 读取 Hash 字段；未命中时返回 ErrMiss。
	HGet(ctx context.Context, key string, field string, dest any) error
	// HGetAll 获取整个 Hash；当前仅支持 *map[string]T 目标类型。
	HGetAll(ctx context.Context, key string, destMap any) error
	// HDel 删除一个或多个 Hash 字段。
	HDel(ctx context.Context, key string, fields ...string) error
	// HIncrBy 原子递增整数类型字段。
	HIncrBy(ctx context.Context, key string, field string, increment int64) (int64, error)
	// ZAdd 向有序集合中写入成员。
	ZAdd(ctx context.Context, key string, score float64, member any) error
	// ZRem 从有序集合中删除成员。
	ZRem(ctx context.Context, key string, members ...any) error
	// ZScore 返回成员分数；未命中时返回 ErrMiss。
	ZScore(ctx context.Context, key string, member any) (float64, error)
	// ZRange 按分数升序返回指定区间内成员。
	ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	// ZRevRange 按分数降序返回指定区间内成员。
	ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	// ZRangeByScore 返回指定分数区间内成员。
	ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error
	// MGet 批量读取多个 key；目标必须是切片指针。
	MGet(ctx context.Context, keys []string, destSlice any) error
	// MSet 批量设置多个 key-value。
	MSet(ctx context.Context, items map[string]any, ttl time.Duration) error
	// RawClient 返回底层客户端，用于 Pipeline、Lua 脚本等高级场景。
	RawClient() any
}

// Local 定义本地缓存能力。
//
// Local 面向进程内热点数据，只提供 KV 能力，并承诺值语义：
// 调用方修改原始对象或读取结果，不应反向污染缓存内部数据。
type Local interface {
	KV
}

// Multi 定义多级缓存能力。
//
// Multi 代表一层策略组合，而不是新的存储引擎。它遵循：
//   - 读路径：local -> distributed -> backfill local。
//   - 写路径：write distributed -> write local。
//   - 删路径：delete distributed + delete local。
type Multi interface {
	KV
}

// NewDistributed 根据配置创建分布式缓存实例。
//
// 当前仅支持 Redis，需要通过 WithRedisConnector 显式注入连接器。
func NewDistributed(cfg *DistributedConfig, opts ...Option) (Distributed, error) {
	if cfg == nil {
		return nil, xerrors.New("cache: distributed config is nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt := buildOptions(opts...)
	if opt.RedisConn == nil {
		return nil, ErrRedisConnectorRequired
	}

	switch cfg.Driver {
	case DriverRedis:
		return newRedis(opt.RedisConn, cfg, opt.Logger, opt.Meter)
	default:
		return nil, xerrors.New("cache: unsupported distributed driver: " + string(cfg.Driver))
	}
}

// NewLocal 根据配置创建本地缓存实例。
//
// 当前默认实现基于 otter，面向进程内热点数据和短路径加速场景。
func NewLocal(cfg *LocalConfig, opts ...Option) (Local, error) {
	if cfg == nil {
		return nil, xerrors.New("cache: local config is nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt := buildOptions(opts...)
	return newLocal(cfg, opt.Logger, opt.Meter)
}

// NewMulti 根据配置创建多级缓存实例。
//
// local 与 remote 是核心依赖，必须显式传入。Multi 不扩展 Hash、ZSet 等远程能力，
// 仅提供两级 KV 策略。
func NewMulti(local Local, remote Distributed, cfg *MultiConfig) (Multi, error) {
	if local == nil {
		return nil, ErrLocalCacheRequired
	}
	if remote == nil {
		return nil, ErrRemoteCacheRequired
	}

	if cfg == nil {
		cfg = &MultiConfig{}
	}
	cfg.setDefaults()
	return newMulti(local, remote, cfg)
}

func buildOptions(opts ...Option) options {
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

	return opt
}
