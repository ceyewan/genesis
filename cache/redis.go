package cache

import (
	"context"
	"reflect"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/cache/serializer"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

type redisCache struct {
	client     *redis.Client
	serializer serializer.Serializer
	prefix     string
	defaultTTL time.Duration
	logger     clog.Logger
	meter      metrics.Meter
}

// newRedis 创建 Redis 缓存实例
func newRedis(conn connector.RedisConnector, cfg *DistributedConfig, logger clog.Logger, meter metrics.Meter) (Distributed, error) {
	if conn == nil {
		return nil, ErrRedisConnectorRequired
	}
	if cfg == nil {
		return nil, xerrors.New("cache: distributed config is nil")
	}

	serializerType := cfg.Serializer
	if serializerType == "" {
		serializerType = "json"
	}

	s, err := serializer.New(serializerType)
	if err != nil {
		return nil, err
	}

	return &redisCache{
		client:     conn.GetClient(),
		serializer: s,
		prefix:     cfg.KeyPrefix,
		defaultTTL: cfg.DefaultTTL,
		logger:     logger,
		meter:      meter,
	}, nil
}

func (c *redisCache) getKey(key string) string {
	return c.prefix + key
}

func (c *redisCache) marshal(value any) ([]byte, error) {
	return c.serializer.Marshal(value)
}

func (c *redisCache) unmarshal(data []byte, dest any) error {
	return c.serializer.Unmarshal(data, dest)
}

// --- 键值（Key-Value） ---

func (c *redisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := c.marshal(value)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	if err := c.client.Set(ctx, c.getKey(key), data, ttl).Err(); err != nil {
		c.logger.ErrorContext(ctx, "Cache set failed", clog.String("key", key), clog.Error(err))
		return err
	}
	return nil
}

func (c *redisCache) Get(ctx context.Context, key string, dest any) error {
	data, err := c.client.Get(ctx, c.getKey(key)).Bytes()
	if err != nil {
		err = normalizeRedisError(err)
		if !xerrors.Is(err, ErrMiss) {
			c.logger.ErrorContext(ctx, "Cache get failed", clog.String("key", key), clog.Error(err))
		}
		return err
	}
	return c.unmarshal(data, dest)
}

func (c *redisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, c.getKey(key)).Err()
}

func (c *redisCache) Has(ctx context.Context, key string) (bool, error) {
	n, err := c.client.Exists(ctx, c.getKey(key)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (c *redisCache) Expire(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	ok, err := c.client.Expire(ctx, c.getKey(key), ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// --- 哈希（Hash） ---

func (c *redisCache) HSet(ctx context.Context, key string, field string, value any) error {
	data, err := c.marshal(value)
	if err != nil {
		return err
	}
	return c.client.HSet(ctx, c.getKey(key), field, data).Err()
}

func (c *redisCache) HGet(ctx context.Context, key string, field string, dest any) error {
	data, err := c.client.HGet(ctx, c.getKey(key), field).Bytes()
	if err != nil {
		return normalizeRedisError(err)
	}
	return c.unmarshal(data, dest)
}

func (c *redisCache) HGetAll(ctx context.Context, key string, destMap any) error {
	result, err := c.client.HGetAll(ctx, c.getKey(key)).Result()
	if err != nil {
		return err
	}

	v := reflect.ValueOf(destMap)
	if v.Kind() != reflect.Pointer {
		return xerrors.New("destMap must be a pointer")
	}
	v = v.Elem()

	if v.Kind() != reflect.Map {
		return xerrors.New("destMap must be a pointer to a map")
	}

	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}
	elemType := v.Type().Elem()
	for k, valStr := range result {
		newElem := reflect.New(elemType)
		if err := c.unmarshal([]byte(valStr), newElem.Interface()); err != nil {
			return err
		}
		v.SetMapIndex(reflect.ValueOf(k), newElem.Elem())
	}
	return nil
}

func (c *redisCache) HDel(ctx context.Context, key string, fields ...string) error {
	return c.client.HDel(ctx, c.getKey(key), fields...).Err()
}

func (c *redisCache) HIncrBy(ctx context.Context, key string, field string, increment int64) (int64, error) {
	return c.client.HIncrBy(ctx, c.getKey(key), field, increment).Result()
}

// --- 有序集合（Sorted Set） ---

func (c *redisCache) ZAdd(ctx context.Context, key string, score float64, member any) error {
	data, err := c.marshal(member)
	if err != nil {
		return err
	}
	return c.client.ZAdd(ctx, c.getKey(key), redis.Z{Score: score, Member: data}).Err()
}

func (c *redisCache) ZRem(ctx context.Context, key string, members ...any) error {
	serializedMembers := make([]any, len(members))
	for i, m := range members {
		data, err := c.marshal(m)
		if err != nil {
			return err
		}
		serializedMembers[i] = string(data)
	}
	return c.client.ZRem(ctx, c.getKey(key), serializedMembers...).Err()
}

func (c *redisCache) ZScore(ctx context.Context, key string, member any) (float64, error) {
	data, err := c.marshal(member)
	if err != nil {
		return 0, err
	}
	score, err := c.client.ZScore(ctx, c.getKey(key), string(data)).Result()
	if err != nil {
		return 0, normalizeRedisError(err)
	}
	return score, nil
}

func (c *redisCache) ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	result, err := c.client.ZRange(ctx, c.getKey(key), start, stop).Result()
	if err != nil {
		return err
	}
	return c.unmarshalSlice(result, destSlice)
}

func (c *redisCache) ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	result, err := c.client.ZRevRange(ctx, c.getKey(key), start, stop).Result()
	if err != nil {
		return err
	}
	return c.unmarshalSlice(result, destSlice)
}

func (c *redisCache) ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error {
	result, err := c.client.ZRangeByScore(ctx, c.getKey(key), &redis.ZRangeBy{
		Min: strconv.FormatFloat(min, 'f', -1, 64),
		Max: strconv.FormatFloat(max, 'f', -1, 64),
	}).Result()
	if err != nil {
		return err
	}
	return c.unmarshalSlice(result, destSlice)
}

// --- 批量操作（Batch Operations） ---

func (c *redisCache) MGet(ctx context.Context, keys []string, destSlice any) error {
	if len(keys) == 0 {
		return nil
	}

	v := reflect.ValueOf(destSlice)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Slice {
		return xerrors.New("destSlice must be a pointer to slice")
	}

	prefixedKeys := make([]string, len(keys))
	for i, k := range keys {
		prefixedKeys[i] = c.getKey(k)
	}

	results, err := c.client.MGet(ctx, prefixedKeys...).Result()
	if err != nil {
		return err
	}

	sliceVal := v.Elem()
	elemType := sliceVal.Type().Elem()
	newSlice := reflect.MakeSlice(sliceVal.Type(), len(results), len(results))

	for i, result := range results {
		elem := newSlice.Index(i)
		if result == nil {
			continue
		}

		data, ok := result.(string)
		if !ok {
			return xerrors.New("unexpected result type from MGET")
		}

		var target any
		if elemType.Kind() == reflect.Pointer {
			val := reflect.New(elemType.Elem())
			target = val.Interface()
			if err := c.unmarshal([]byte(data), target); err != nil {
				return err
			}
			elem.Set(val)
		} else {
			target = elem.Addr().Interface()
			if err := c.unmarshal([]byte(data), target); err != nil {
				return err
			}
		}
	}

	sliceVal.Set(newSlice)
	return nil
}

func (c *redisCache) MSet(ctx context.Context, items map[string]any, ttl time.Duration) error {
	if len(items) == 0 {
		return nil
	}

	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	pipe := c.client.Pipeline()
	for k, v := range items {
		data, err := c.marshal(v)
		if err != nil {
			return err
		}
		pipe.Set(ctx, c.getKey(k), data, ttl)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// --- 高级操作（Advanced） ---

// RawClient 返回底层 Redis 客户端，用于执行 Pipeline、Lua 脚本等高级操作。
func (c *redisCache) RawClient() any {
	return c.client
}

// --- 工具与辅助函数 ---

// Close 是 no-op：Cache 不拥有 Redis 连接，由 Connector 管理。
func (c *redisCache) Close() error {
	return nil
}

func normalizeRedisError(err error) error {
	if err == nil {
		return nil
	}
	if err == redis.Nil {
		return ErrMiss
	}
	return err
}

func (c *redisCache) unmarshalSlice(data []string, destSlice any) error {
	v := reflect.ValueOf(destSlice)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Slice {
		return xerrors.New("destSlice must be a pointer to slice")
	}
	sliceVal := v.Elem()
	elemType := sliceVal.Type().Elem()

	newSlice := reflect.MakeSlice(sliceVal.Type(), len(data), len(data))

	for i, s := range data {
		elem := newSlice.Index(i)

		var target any
		if elemType.Kind() == reflect.Pointer {
			val := reflect.New(elemType.Elem())
			target = val.Interface()
			elem.Set(val)
		} else {
			target = elem.Addr().Interface()
		}

		if err := c.unmarshal([]byte(s), target); err != nil {
			return err
		}
	}

	sliceVal.Set(newSlice)
	return nil
}
