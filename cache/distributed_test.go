package cache

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/testkit"
	"github.com/stretchr/testify/require"
)

// setupTestDistributed 创建用于测试的分布式缓存实例
func setupTestDistributed(t *testing.T, prefix string) Distributed {
	redisConn := newRedisConnectorOrSkip(t)

	ctx := context.Background()
	client := redisConn.GetClient()
	keys, _ := client.Keys(ctx, prefix+"*").Result()
	if len(keys) > 0 {
		client.Del(ctx, keys...)
	}

	logger := clog.Discard()

	dist, err := NewDistributed(&DistributedConfig{
		Driver:     DriverRedis,
		KeyPrefix:  prefix,
		Serializer: "json",
		DefaultTTL: time.Hour,
	}, WithRedisConnector(redisConn), WithLogger(logger))
	require.NoError(t, err)

	return dist
}

func newRedisConnectorOrSkip(t *testing.T) (conn connector.RedisConnector) {
	t.Helper()

	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skipf("skip redis integration test: docker unavailable: %v", err)
	}

	return testkit.NewRedisContainerConnector(t)
}

// TestDistributed_KV_Integration 测试分布式缓存的 KV 操作
func TestDistributed_KV_Integration(t *testing.T) {
	cache := setupTestDistributed(t, "test:dist:kv:")
	ctx := context.Background()

	t.Run("Set and Get", func(t *testing.T) {
		value := map[string]any{"name": "alice", "age": 30}
		err := cache.Set(ctx, "user:1", value, time.Minute)
		require.NoError(t, err)

		var got map[string]any
		err = cache.Get(ctx, "user:1", &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
		require.Equal(t, float64(30), got["age"])
	})

	t.Run("Get non-existent key returns ErrMiss", func(t *testing.T) {
		var got string
		err := cache.Get(ctx, "nonexistent", &got)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("Delete", func(t *testing.T) {
		err := cache.Set(ctx, "user:2", "value", time.Minute)
		require.NoError(t, err)

		err = cache.Delete(ctx, "user:2")
		require.NoError(t, err)

		var got string
		err = cache.Get(ctx, "user:2", &got)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("Has", func(t *testing.T) {
		err := cache.Set(ctx, "user:3", "value", time.Minute)
		require.NoError(t, err)

		ok, err := cache.Has(ctx, "user:3")
		require.NoError(t, err)
		require.True(t, ok)

		ok, err = cache.Has(ctx, "nonexistent")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("Expire", func(t *testing.T) {
		err := cache.Set(ctx, "user:4", "value", time.Minute)
		require.NoError(t, err)

		// 成功更新 TTL
		ok, err := cache.Expire(ctx, "user:4", 10*time.Minute)
		require.NoError(t, err)
		require.True(t, ok)

		// key 不存在返回 false, nil
		ok, err = cache.Expire(ctx, "nonexistent", 10*time.Minute)
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("DefaultTTL", func(t *testing.T) {
		// ttl=0 时使用 DefaultTTL
		err := cache.Set(ctx, "user:5", "value", 0)
		require.NoError(t, err)

		var got string
		err = cache.Get(ctx, "user:5", &got)
		require.NoError(t, err)
		require.Equal(t, "value", got)
	})

	t.Run("RawClient", func(t *testing.T) {
		client := cache.RawClient()
		require.NotNil(t, client)
	})
}

// TestDistributed_ErrorHandling_Integration 测试错误处理
func TestDistributed_ErrorHandling_Integration(t *testing.T) {
	cache := setupTestDistributed(t, "test:dist:err:")
	ctx := context.Background()

	t.Run("Get returns ErrMiss for non-existent key", func(t *testing.T) {
		var got string
		err := cache.Get(ctx, "nonexistent", &got)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("Has returns (false, nil) for non-existent key", func(t *testing.T) {
		ok, err := cache.Has(ctx, "nonexistent")
		require.NoError(t, err)
		require.False(t, ok)
	})

	t.Run("Expire returns (false, nil) for non-existent key", func(t *testing.T) {
		ok, err := cache.Expire(ctx, "nonexistent", time.Minute)
		require.NoError(t, err)
		require.False(t, ok)
	})
}
