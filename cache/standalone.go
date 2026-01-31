package cache

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/maypok86/otter/v2"
	"github.com/maypok86/otter/v2/stats"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

type standaloneCache struct {
	cache  *otter.Cache[string, any]
	logger clog.Logger
	meter  metrics.Meter
}

// newStandalone 创建单机内存缓存实例
func newStandalone(cfg *StandaloneConfig, logger clog.Logger, meter metrics.Meter) (Cache, error) {
	if cfg == nil {
		cfg = &StandaloneConfig{Capacity: 10000}
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 10000
	}

	// Otter v2 配置
	opts := &otter.Options[string, any]{
		MaximumSize:   cfg.Capacity,
		StatsRecorder: stats.NewCounter(),
		// 使用写入过期策略（与 Redis TTL 语义一致）：
		// - 过期时间从写入开始计算
		// - 读取不会重置 TTL
		// 具体 TTL 将在 Set 时通过 SetExpiresAfter 覆盖
		ExpiryCalculator: otter.ExpiryWriting[string, any](defaultTTL),
	}

	cache, err := otter.New(opts)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to build otter cache")
	}

	return &standaloneCache{
		cache:  cache,
		logger: logger,
		meter:  meter,
	}, nil
}

// --- 键值（Key-Value） ---

const (
	// defaultTTL 当未指定 TTL 时使用的默认时间（100年，模拟永久）
	defaultTTL = 24 * 365 * 100 * time.Hour
)

func (c *standaloneCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	// Set 返回是否成功
	c.cache.Set(key, value)

	// 如果指定了有效 TTL，则覆盖默认过期时间
	if ttl > 0 {
		c.cache.SetExpiresAfter(key, ttl)
	}
	return nil
}

func (c *standaloneCache) Get(ctx context.Context, key string, dest any) error {
	// v2 使用 GetIfPresent 获取缓存值（无 loader）
	val, ok := c.cache.GetIfPresent(key)
	if !ok {
		return xerrors.New("cache miss")
	}

	// 由于本地缓存存储的是原始对象，这里需要处理 dest
	// 如果 dest 是指针，将 val 赋值给 dest 指向的内容
	return c.assignValue(val, dest)
}

func (c *standaloneCache) Delete(ctx context.Context, key string) error {
	c.cache.Invalidate(key)
	return nil
}

func (c *standaloneCache) Has(ctx context.Context, key string) (bool, error) {
	// Otter v2 移除了 Has 方法，使用 GetIfPresent 代替
	// 注意：这可能会有副作用（如更新 LRU/TTL，取决于配置），但在 otter 中 GetIfPresent 通常是安全的
	_, ok := c.cache.GetIfPresent(key)
	return ok, nil
}

func (c *standaloneCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	// 确认 key 是否存在
	if _, ok := c.cache.GetIfPresent(key); !ok {
		return xerrors.New("cache miss")
	}

	c.cache.SetExpiresAfter(key, ttl)
	return nil
}

// --- 不支持的操作 (返回错误) ---

var errNotSupported = xerrors.New("operation not supported in standalone mode")

func (c *standaloneCache) HSet(ctx context.Context, key string, field string, value any) error {
	return errNotSupported
}

func (c *standaloneCache) HGet(ctx context.Context, key string, field string, dest any) error {
	return errNotSupported
}

func (c *standaloneCache) HGetAll(ctx context.Context, key string, destMap any) error {
	return errNotSupported
}

func (c *standaloneCache) HDel(ctx context.Context, key string, fields ...string) error {
	return errNotSupported
}

func (c *standaloneCache) HIncrBy(ctx context.Context, key string, field string, increment int64) (int64, error) {
	return 0, errNotSupported
}

func (c *standaloneCache) ZAdd(ctx context.Context, key string, score float64, member any) error {
	return errNotSupported
}

func (c *standaloneCache) ZRem(ctx context.Context, key string, members ...any) error {
	return errNotSupported
}

func (c *standaloneCache) ZScore(ctx context.Context, key string, member any) (float64, error) {
	return 0, errNotSupported
}

func (c *standaloneCache) ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return errNotSupported
}

func (c *standaloneCache) ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return errNotSupported
}

func (c *standaloneCache) ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error {
	return errNotSupported
}

func (c *standaloneCache) LPush(ctx context.Context, key string, values ...any) error {
	return errNotSupported
}

func (c *standaloneCache) RPush(ctx context.Context, key string, values ...any) error {
	return errNotSupported
}

func (c *standaloneCache) LPop(ctx context.Context, key string, dest any) error {
	return errNotSupported
}

func (c *standaloneCache) RPop(ctx context.Context, key string, dest any) error {
	return errNotSupported
}

func (c *standaloneCache) LRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	return errNotSupported
}

func (c *standaloneCache) LPushCapped(ctx context.Context, key string, limit int64, values ...any) error {
	return errNotSupported
}

// --- 批量操作（Batch Operations） ---

func (c *standaloneCache) MGet(ctx context.Context, keys []string, destSlice any) error {
	return errNotSupported
}

func (c *standaloneCache) MSet(ctx context.Context, items map[string]any, ttl time.Duration) error {
	return errNotSupported
}

// --- 高级操作（Advanced） ---

// Client 返回 nil，因为 Memory 驱动不使用 Redis 客户端
func (c *standaloneCache) Client() any {
	return nil
}

// --- 工具与辅助函数 ---

func (c *standaloneCache) Close() error {
	c.cache.StopAllGoroutines()
	return nil
}

func (c *standaloneCache) assignValue(val any, dest any) error {
	// 【注意】本地缓存的引用安全问题
	// 这是一个基于反射的浅拷贝实现。
	// 如果缓存的对象包含指针（如 *struct, map, slice），dest 将指向与缓存中相同的底层数据。
	// 如果用户修改了 dest 中的数据，缓存中的数据也会被修改。
	//
	// 为了保持高性能（避免昂贵的深拷贝），我们有意保留这种行为。
	// 用户应当将从缓存获取的对象视为"只读"，或者在修改前自行进行深拷贝。

	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("dest must be a non-nil pointer")
	}

	// 解引用 dest，获取它指向的值
	dv = dv.Elem()

	sv := reflect.ValueOf(val)

	// 1. 类型完全匹配或可赋值（这是最常见的情况）
	if sv.Type().AssignableTo(dv.Type()) {
		dv.Set(sv)
		return nil
	}

	// 2. 如果 dest 是 interface{} (例如: var res interface{}; Get(..., &res))
	// 我们可以将任何值赋给它
	if dv.Kind() == reflect.Interface && sv.Type().Implements(dv.Type()) {
		dv.Set(sv)
		return nil
	}

	// 3. 处理数字类型的转换（例如存的是 int，取的是 int64，类似于 JSON 反序列化的宽容性）
	// 这是一个额外的便利特性，使本地缓存的行为更接近 JSON 序列化器
	if isNumber(sv.Kind()) && isNumber(dv.Kind()) {
		// 转换并赋值
		if dv.OverflowInt(sv.Int()) {
			return fmt.Errorf("value overflow when assigning %v to %T", val, dest)
		}
		// 这里简化处理，仅支持 int 系列，完整实现需要处理 float/uint
		// 鉴于性能考虑，如果类型不严格匹配，返回错误是合理的。
		// 这里暂不深入实现复杂的数字转换。
	}

	return fmt.Errorf("cannot assign cached value of type %T to dest of type %T", val, dest)
}

func isNumber(k reflect.Kind) bool {
	return k >= reflect.Int && k <= reflect.Float64
}
