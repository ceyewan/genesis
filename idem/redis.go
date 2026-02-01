package idem

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// redisStore Redis 存储实现（非导出）
type redisStore struct {
	client connector.RedisConnector
	prefix string
}

// newRedisStore 创建 Redis 存储实例（内部函数）
func newRedisStore(redisConn connector.RedisConnector, prefix string) Store {
	return &redisStore{
		client: redisConn,
		prefix: prefix,
	}
}

// Lock 尝试获取锁（标记处理中）
func (rs *redisStore) Lock(ctx context.Context, key string, ttl time.Duration) (LockToken, bool, error) {
	lockKey := rs.prefix + key + lockSuffix

	token, err := newLockToken()
	if err != nil {
		return "", false, err
	}

	// 使用 SET NX 原子操作获取锁
	result, err := rs.client.GetClient().SetNX(ctx, lockKey, string(token), ttl).Result()
	if err != nil && err != redis.Nil {
		return "", false, xerrors.Wrap(err, "failed to acquire lock")
	}

	if !result {
		return "", false, nil
	}

	return token, true, nil
}

// Unlock 释放锁
func (rs *redisStore) Unlock(ctx context.Context, key string, token LockToken) error {
	if token == "" {
		return nil
	}
	lockKey := rs.prefix + key + lockSuffix

	_, err := redisUnlockScript.Run(ctx, rs.client.GetClient(), []string{lockKey}, string(token)).Result()
	if err != nil && err != redis.Nil {
		return xerrors.Wrap(err, "failed to release lock")
	}

	return nil
}

// SetResult 保存执行结果并标记完成
func (rs *redisStore) SetResult(ctx context.Context, key string, val []byte, ttl time.Duration, token LockToken) error {
	resultKey := rs.prefix + key + resultSuffix
	lockKey := rs.prefix + key + lockSuffix

	ttlMs := ttl.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = int64(time.Second / time.Millisecond)
	}

	if token == "" {
		if err := rs.client.GetClient().Set(ctx, resultKey, val, ttl).Err(); err != nil {
			return xerrors.Wrap(err, "failed to set result")
		}
		return nil
	}

	_, err := redisSetResultScript.Run(
		ctx,
		rs.client.GetClient(),
		[]string{resultKey, lockKey},
		val,
		ttlMs,
		string(token),
	).Result()
	if err != nil {
		return xerrors.Wrap(err, "failed to set result")
	}

	return nil
}

// GetResult 获取已完成的结果
func (rs *redisStore) GetResult(ctx context.Context, key string) ([]byte, error) {
	resultKey := rs.prefix + key + resultSuffix

	result, err := rs.client.GetClient().Get(ctx, resultKey).Bytes()
	if err == redis.Nil {
		return nil, ErrResultNotFound
	}
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to get result")
	}

	return result, nil
}

// Refresh 刷新锁 TTL（仅当 token 匹配时生效）
func (rs *redisStore) Refresh(ctx context.Context, key string, token LockToken, ttl time.Duration) error {
	if token == "" {
		return nil
	}
	lockKey := rs.prefix + key + lockSuffix
	ttlMs := ttl.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = int64(time.Second / time.Millisecond)
	}

	_, err := redisRefreshScript.Run(ctx, rs.client.GetClient(), []string{lockKey}, string(token), ttlMs).Result()
	if err != nil && err != redis.Nil {
		return xerrors.Wrap(err, "failed to refresh lock")
	}

	return nil
}

var (
	redisUnlockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)
	redisRefreshScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)
	redisSetResultScript = redis.NewScript(`
redis.call("PSETEX", KEYS[1], ARGV[2], ARGV[1])
if ARGV[3] ~= "" then
	if redis.call("GET", KEYS[2]) == ARGV[3] then
		redis.call("DEL", KEYS[2])
	end
end
return 1
`)
)
