package idem

import (
	"context"
	"errors"
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
	resultKey := rs.prefix + key + resultSuffix

	token, err := newLockToken()
	if err != nil {
		return "", false, err
	}
	ttlMs := ttl.Milliseconds()
	if ttlMs <= 0 {
		ttlMs = int64(time.Second / time.Millisecond)
	}

	// 在同一段 Lua 中检查已完成结果并尝试抢锁，避免结果已经发布后又
	// 短暂创建一个新的 processing 锁。
	result, err := redisLockScript.Run(
		ctx,
		rs.client.GetClient(),
		[]string{lockKey, resultKey},
		string(token),
		ttlMs,
	).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", false, xerrors.Wrap(err, "failed to acquire lock")
	}

	if result == 0 {
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
	if err != nil && !errors.Is(err, redis.Nil) {
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

	result, err := redisSetResultScript.Run(
		ctx,
		rs.client.GetClient(),
		[]string{resultKey, lockKey},
		val,
		ttlMs,
		string(token),
	).Int64()
	if err != nil {
		return xerrors.Wrap(err, "failed to set result")
	}
	if result == 0 {
		return ErrLockLost
	}

	return nil
}

// GetResult 获取已完成的结果
func (rs *redisStore) GetResult(ctx context.Context, key string) ([]byte, error) {
	resultKey := rs.prefix + key + resultSuffix

	result, err := rs.client.GetClient().Get(ctx, resultKey).Bytes()
	if errors.Is(err, redis.Nil) {
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

	result, err := redisRefreshScript.Run(ctx, rs.client.GetClient(), []string{lockKey}, string(token), ttlMs).Int64()
	if err != nil && !errors.Is(err, redis.Nil) {
		return xerrors.Wrap(err, "failed to refresh lock")
	}
	if result == 0 {
		return ErrLockLost
	}

	return nil
}

func (rs *redisStore) DeleteResult(ctx context.Context, key string) error {
	resultKey := rs.prefix + key + resultSuffix
	if err := rs.client.GetClient().Del(ctx, resultKey).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return xerrors.Wrap(err, "failed to delete result")
	}
	return nil
}

var (
	redisLockScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[2]) == 1 then
	return 0
end
if redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2], "NX") then
	return 1
end
return 0
`)
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
if ARGV[3] ~= "" then
	if redis.call("GET", KEYS[2]) ~= ARGV[3] then
		return 0
	end
end

redis.call("PSETEX", KEYS[1], ARGV[2], ARGV[1])
if ARGV[3] ~= "" then
	redis.call("DEL", KEYS[2])
end
return 1
`)
)
