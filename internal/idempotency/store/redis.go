package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idempotency/types"
)

// Record 存储在 Redis 中的幂等记录结构
type Record struct {
	Status types.Status `json:"status"`
	Result string       `json:"result,omitempty"` // JSON 序列化的结果
}

// RedisStore Redis 存储实现
type RedisStore struct {
	client *redis.Client
	prefix string
}

// NewRedisStore 创建 Redis 存储实例
func NewRedisStore(conn connector.RedisConnector, prefix string) *RedisStore {
	if prefix == "" {
		prefix = "idempotency:"
	}
	return &RedisStore{
		client: conn.GetClient(),
		prefix: prefix,
	}
}

// getKey 获取完整的 Redis key
func (s *RedisStore) getKey(key string) string {
	return s.prefix + key
}

// Lock 尝试加锁（设置 Processing 状态）
// 返回: locked (是否成功加锁), status (当前状态), error
func (s *RedisStore) Lock(ctx context.Context, key string, ttl time.Duration) (bool, types.Status, error) {
	fullKey := s.getKey(key)

	// 构造 Processing 状态的记录
	record := Record{
		Status: types.StatusProcessing,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return false, 0, fmt.Errorf("marshal record: %w", err)
	}

	// 尝试使用 SET NX 加锁
	ok, err := s.client.SetNX(ctx, fullKey, data, ttl).Result()
	if err != nil {
		return false, 0, fmt.Errorf("redis setnx: %w", err)
	}

	if ok {
		// 加锁成功
		return true, types.StatusProcessing, nil
	}

	// 加锁失败，获取当前状态
	status, _, err := s.Get(ctx, key)
	if err != nil {
		return false, 0, err
	}

	return false, status, nil
}

// Get 获取幂等记录
// 返回: status, result (JSON 字符串), error
func (s *RedisStore) Get(ctx context.Context, key string) (types.Status, string, error) {
	fullKey := s.getKey(key)

	data, err := s.client.Get(ctx, fullKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return 0, "", types.ErrNotFound
		}
		return 0, "", fmt.Errorf("redis get: %w", err)
	}

	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return 0, "", fmt.Errorf("unmarshal record: %w", err)
	}

	return record.Status, record.Result, nil
}

// Unlock 解锁并更新状态
// 使用 Lua 脚本确保原子性
func (s *RedisStore) Unlock(ctx context.Context, key string, status types.Status, result string, ttl time.Duration) error {
	fullKey := s.getKey(key)

	// 构造新的记录
	record := Record{
		Status: status,
		Result: result,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}

	// Lua 脚本：只有当前状态是 Processing 时才更新
	script := `
		local key = KEYS[1]
		local new_value = ARGV[1]
		local ttl = ARGV[2]
		
		local current = redis.call('GET', key)
		if not current then
			return redis.error_reply('key not found')
		end
		
		local record = cjson.decode(current)
		if record.status ~= 0 then
			return redis.error_reply('status is not processing')
		end
		
		redis.call('SET', key, new_value, 'EX', ttl)
		return 'OK'
	`

	err = s.client.Eval(ctx, script, []string{fullKey}, data, int(ttl.Seconds())).Err()
	if err != nil {
		return fmt.Errorf("redis unlock: %w", err)
	}

	return nil
}

// Delete 删除幂等记录
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	fullKey := s.getKey(key)
	return s.client.Del(ctx, fullKey).Err()
}
