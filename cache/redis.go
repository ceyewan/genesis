package cache

import (
	"context"
	"fmt"
	"reflect"
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
	logger     clog.Logger
	meter      metrics.Meter
}

// newRedis 创建 Redis 缓存实例
func newRedis(conn connector.RedisConnector, cfg *Config, logger clog.Logger, meter metrics.Meter) (Cache, error) {
	if conn == nil {
		return nil, xerrors.New("redis connector is nil")
	}
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}

	// 设置默认序列化器
	serializerType := cfg.Serializer
	if serializerType == "" {
		serializerType = "json" // 默认使用 JSON
	}

	s, err := serializer.New(serializerType)
	if err != nil {
		return nil, err
	}

	return &redisCache{
		client:     conn.GetClient(),
		serializer: s,
		prefix:     cfg.Prefix,
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
	return c.client.Set(ctx, c.getKey(key), data, ttl).Err()
}

func (c *redisCache) Get(ctx context.Context, key string, dest any) error {
	data, err := c.client.Get(ctx, c.getKey(key)).Bytes()
	if err != nil {
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

func (c *redisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.client.Expire(ctx, c.getKey(key), ttl).Err()
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
		return err
	}
	return c.unmarshal(data, dest)
}

func (c *redisCache) HGetAll(ctx context.Context, key string, destMap any) error {
	result, err := c.client.HGetAll(ctx, c.getKey(key)).Result()
	if err != nil {
		return err
	}

	v := reflect.ValueOf(destMap)
	if v.Kind() != reflect.Ptr {
		return xerrors.New("destMap must be a pointer")
	}
	v = v.Elem()

	if v.Kind() == reflect.Map {
		// destMap 是 *map[string]T
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
	} else if v.Kind() == reflect.Struct {
		// destMap 是 *struct
		// 这里采用简化实现：假设结构体字段名与哈希键一致，
		// 并把哈希的值反序列化到对应的结构体字段中。
		// 更健壮的实现应使用 struct tag 来映射字段名与键名。
		// 目前为保证安全和简单性，优先支持 map（示例使用 map[string]string），
		// 若要完整支持 struct 需要更复杂的映射逻辑，因此这里返回错误。
		return xerrors.New("HGetAll currently only supports pointer to map")
	}

	return xerrors.New("destMap must be a pointer to a map")
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
		serializedMembers[i] = data
	}
	return c.client.ZRem(ctx, c.getKey(key), serializedMembers...).Err()
}

func (c *redisCache) ZScore(ctx context.Context, key string, member any) (float64, error) {
	data, err := c.marshal(member)
	if err != nil {
		return 0, err
	}
	return c.client.ZScore(ctx, c.getKey(key), string(data)).Result()
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
		Min: fmt.Sprintf("%f", min),
		Max: fmt.Sprintf("%f", max),
	}).Result()
	if err != nil {
		return err
	}
	return c.unmarshalSlice(result, destSlice)
}

// --- 列表（List） ---

func (c *redisCache) LPush(ctx context.Context, key string, values ...any) error {
	serializedValues := make([]any, len(values))
	for i, v := range values {
		data, err := c.marshal(v)
		if err != nil {
			return err
		}
		serializedValues[i] = data
	}
	return c.client.LPush(ctx, c.getKey(key), serializedValues...).Err()
}

func (c *redisCache) RPush(ctx context.Context, key string, values ...any) error {
	serializedValues := make([]any, len(values))
	for i, v := range values {
		data, err := c.marshal(v)
		if err != nil {
			return err
		}
		serializedValues[i] = data
	}
	return c.client.RPush(ctx, c.getKey(key), serializedValues...).Err()
}

func (c *redisCache) LPop(ctx context.Context, key string, dest any) error {
	data, err := c.client.LPop(ctx, c.getKey(key)).Bytes()
	if err != nil {
		return err
	}
	return c.unmarshal(data, dest)
}

func (c *redisCache) RPop(ctx context.Context, key string, dest any) error {
	data, err := c.client.RPop(ctx, c.getKey(key)).Bytes()
	if err != nil {
		return err
	}
	return c.unmarshal(data, dest)
}

func (c *redisCache) LRange(ctx context.Context, key string, start, stop int64, destSlice any) error {
	result, err := c.client.LRange(ctx, c.getKey(key), start, stop).Result()
	if err != nil {
		return err
	}
	return c.unmarshalSlice(result, destSlice)
}

func (c *redisCache) LPushCapped(ctx context.Context, key string, limit int64, values ...any) error {
	serializedValues := make([]any, len(values))
	for i, v := range values {
		data, err := c.marshal(v)
		if err != nil {
			return err
		}
		serializedValues[i] = data
	}

	pipe := c.client.Pipeline()
	pipe.LPush(ctx, c.getKey(key), serializedValues...)
	pipe.LTrim(ctx, c.getKey(key), 0, limit-1)
	_, err := pipe.Exec(ctx)
	return err
}

// --- 批量操作（Batch Operations） ---

func (c *redisCache) MGet(ctx context.Context, keys []string, destSlice any) error {
	if len(keys) == 0 {
		return nil
	}

	// 验证 destSlice 必须是指向切片的指针
	v := reflect.ValueOf(destSlice)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Slice {
		return xerrors.New("destSlice must be a pointer to slice")
	}

	// 添加前缀
	prefixedKeys := make([]string, len(keys))
	for i, k := range keys {
		prefixedKeys[i] = c.getKey(k)
	}

	// 执行 MGET
	results, err := c.client.MGet(ctx, prefixedKeys...).Result()
	if err != nil {
		return err
	}

	// 反序列化结果
	sliceVal := v.Elem()
	elemType := sliceVal.Type().Elem()
	newSlice := reflect.MakeSlice(sliceVal.Type(), len(results), len(results))

	for i, result := range results {
		elem := newSlice.Index(i)
		if result == nil {
			// key 不存在，保留零值
			continue
		}

		// result 是 string 类型
		data, ok := result.(string)
		if !ok {
			return xerrors.New("unexpected result type from MGET")
		}

		var target any
		if elemType.Kind() == reflect.Ptr {
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

// Client 返回底层 Redis 客户端，用于执行 Pipeline、Lua 脚本等高级操作
func (c *redisCache) Client() any {
	return c.client
}

// --- 工具与辅助函数 ---

func (c *redisCache) Close() error {
	// No-op: Cache 不拥有 Redis 连接，由 Connector 管理
	// 调用方应关闭 Connector 而非 Cache
	return nil
}

// 辅助函数：将字符串切片反序列化为对象切片
func (c *redisCache) unmarshalSlice(data []string, destSlice any) error {
	v := reflect.ValueOf(destSlice)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Slice {
		return xerrors.New("destSlice must be a pointer to slice")
	}
	sliceVal := v.Elem()
	elemType := sliceVal.Type().Elem()

	// 创建新切片来保存结果
	newSlice := reflect.MakeSlice(sliceVal.Type(), len(data), len(data))

	for i, s := range data {
		elem := newSlice.Index(i)

		// 我们需要传递指针给 Unmarshal
		// 如果 elem 是指针类型（例如 *User），elem.Interface() 是 nil *User
		// 我们需要分配 User

		var target any
		if elemType.Kind() == reflect.Ptr {
			// elemType 是 *T
			// 分配 T
			val := reflect.New(elemType.Elem()) // val 是 *T
			target = val.Interface()
			// 设置 elem 为 val
			elem.Set(val)
		} else {
			// elemType 是 T
			// elem.Addr() 是 *T
			target = elem.Addr().Interface()
		}

		if err := c.unmarshal([]byte(s), target); err != nil {
			return err
		}
	}

	sliceVal.Set(newSlice)
	return nil
}
