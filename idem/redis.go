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
func (rs *redisStore) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	lockKey := rs.prefix + key + lockSuffix

	// 使用 SET NX 原子操作获取锁
	result, err := rs.client.GetClient().SetNX(ctx, lockKey, "1", ttl).Result()
	if err != nil && err != redis.Nil {
		return false, xerrors.Wrap(err, "failed to acquire lock")
	}

	return result, nil
}

// Unlock 释放锁
func (rs *redisStore) Unlock(ctx context.Context, key string) error {
	lockKey := rs.prefix + key + lockSuffix

	_, err := rs.client.GetClient().Del(ctx, lockKey).Result()
	if err != nil {
		return xerrors.Wrap(err, "failed to release lock")
	}

	return nil
}

// SetResult 保存执行结果并标记完成
func (rs *redisStore) SetResult(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	resultKey := rs.prefix + key + resultSuffix
	lockKey := rs.prefix + key + lockSuffix

	// 使用 pipeline 原子操作：设置结果并删除锁
	pipe := rs.client.GetClient().Pipeline()
	pipe.Set(ctx, resultKey, val, ttl)
	pipe.Del(ctx, lockKey)

	_, err := pipe.Exec(ctx)
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
