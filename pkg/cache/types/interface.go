package types

import (
	"context"
	"time"
)

// Cache 定义了缓存组件的核心能力
type Cache interface {
	// --- Key-Value ---
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Get(ctx context.Context, key string, dest any) error
	Delete(ctx context.Context, key string) error
	Has(ctx context.Context, key string) (bool, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// --- Hash ---
	HSet(ctx context.Context, key string, field string, value any) error
	HGet(ctx context.Context, key string, field string, dest any) error
	HGetAll(ctx context.Context, key string, destMap any) error
	HDel(ctx context.Context, key string, fields ...string) error
	HIncrBy(ctx context.Context, key string, field string, increment int64) (int64, error)

	// --- Sorted Set ---
	ZAdd(ctx context.Context, key string, score float64, member any) error
	ZRem(ctx context.Context, key string, members ...any) error
	ZScore(ctx context.Context, key string, member any) (float64, error)
	ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error

	// --- List ---
	LPush(ctx context.Context, key string, values ...any) error
	RPush(ctx context.Context, key string, values ...any) error
	LPop(ctx context.Context, key string, dest any) error
	RPop(ctx context.Context, key string, dest any) error
	LRange(ctx context.Context, key string, start, stop int64, destSlice any) error
	LPushCapped(ctx context.Context, key string, limit int64, values ...any) error

	// --- Utility ---
	Close() error
}
