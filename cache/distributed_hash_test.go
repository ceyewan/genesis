package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDistributed_Hash_Integration 测试 Hash 操作
func TestDistributed_Hash_Integration(t *testing.T) {
	cache := setupTestDistributed(t, "test:dist:hash:")
	ctx := context.Background()

	t.Run("HSet and HGet", func(t *testing.T) {
		value := map[string]string{"field1": "value1"}
		err := cache.HSet(ctx, "hash:1", "field1", value)
		require.NoError(t, err)

		var got map[string]string
		err = cache.HGet(ctx, "hash:1", "field1", &got)
		require.NoError(t, err)
		require.Equal(t, "value1", got["field1"])
	})

	t.Run("HGet non-existent field returns ErrMiss", func(t *testing.T) {
		var got string
		err := cache.HGet(ctx, "hash:1", "nonexistent", &got)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("HGetAll", func(t *testing.T) {
		err := cache.HSet(ctx, "hash:2", "name", "alice")
		require.NoError(t, err)
		err = cache.HSet(ctx, "hash:2", "age", "30")
		require.NoError(t, err)

		var got map[string]string
		err = cache.HGetAll(ctx, "hash:2", &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
		require.Equal(t, "30", got["age"])
	})

	t.Run("HGetAll with non-existent key returns empty map", func(t *testing.T) {
		var got map[string]string
		err := cache.HGetAll(ctx, "hash:nonexistent", &got)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("HDel", func(t *testing.T) {
		err := cache.HSet(ctx, "hash:3", "field1", "value1")
		require.NoError(t, err)
		err = cache.HSet(ctx, "hash:3", "field2", "value2")
		require.NoError(t, err)

		err = cache.HDel(ctx, "hash:3", "field1")
		require.NoError(t, err)

		var got map[string]string
		err = cache.HGetAll(ctx, "hash:3", &got)
		require.NoError(t, err)
		require.NotContains(t, got, "field1")
		require.Contains(t, got, "field2")
	})

	t.Run("HIncrBy", func(t *testing.T) {
		// 初始值
		err := cache.HSet(ctx, "hash:4", "counter", 0)
		require.NoError(t, err)

		// 递增
		result, err := cache.HIncrBy(ctx, "hash:4", "counter", 5)
		require.NoError(t, err)
		require.Equal(t, int64(5), result)

		result, err = cache.HIncrBy(ctx, "hash:4", "counter", 3)
		require.NoError(t, err)
		require.Equal(t, int64(8), result)

		// 递减
		result, err = cache.HIncrBy(ctx, "hash:4", "counter", -2)
		require.NoError(t, err)
		require.Equal(t, int64(6), result)
	})

	t.Run("HIncrBy with non-existent field", func(t *testing.T) {
		// 从 0 开始递增
		result, err := cache.HIncrBy(ctx, "hash:5", "counter", 10)
		require.NoError(t, err)
		require.Equal(t, int64(10), result)
	})

	t.Run("Complex value types", func(t *testing.T) {
		type User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		user := User{ID: 1, Name: "alice"}
		err := cache.HSet(ctx, "hash:6", "user", user)
		require.NoError(t, err)

		var got User
		err = cache.HGet(ctx, "hash:6", "user", &got)
		require.NoError(t, err)
		require.Equal(t, 1, got.ID)
		require.Equal(t, "alice", got.Name)
	})
}
