package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestDistributed_Batch_Integration 测试批量操作
func TestDistributed_Batch_Integration(t *testing.T) {
	cache := setupTestDistributed(t, "test:dist:batch:")
	ctx := context.Background()

	t.Run("MSet and MGet", func(t *testing.T) {
		items := map[string]any{
			"user:1": map[string]string{"name": "alice"},
			"user:2": map[string]string{"name": "bob"},
			"user:3": map[string]string{"name": "charlie"},
		}

		err := cache.MSet(ctx, items, time.Minute)
		require.NoError(t, err)

		keys := []string{"user:1", "user:2", "user:3"}
		var results []map[string]string
		err = cache.MGet(ctx, keys, &results)
		require.NoError(t, err)
		require.Len(t, results, 3)
		require.Equal(t, "alice", results[0]["name"])
		require.Equal(t, "bob", results[1]["name"])
		require.Equal(t, "charlie", results[2]["name"])
	})

	t.Run("MGet with non-existent keys", func(t *testing.T) {
		// 先设置一些值
		items := map[string]any{
			"exist:1": "value1",
			"exist:2": "value2",
		}
		err := cache.MSet(ctx, items, time.Minute)
		require.NoError(t, err)

		// MGet 包含存在和不存在的 key
		keys := []string{"exist:1", "nonexistent", "exist:2"}
		var results []string
		err = cache.MGet(ctx, keys, &results)
		require.NoError(t, err)
		require.Len(t, results, 3)
		require.Equal(t, "value1", results[0])
		require.Equal(t, "", results[1]) // 不存在的 key 返回零值
		require.Equal(t, "value2", results[2])
	})

	t.Run("MSet with empty items", func(t *testing.T) {
		err := cache.MSet(ctx, map[string]any{}, time.Minute)
		require.NoError(t, err)
	})

	t.Run("MGet with empty keys", func(t *testing.T) {
		var results []string
		err := cache.MGet(ctx, []string{}, &results)
		require.NoError(t, err)
		require.Empty(t, results)
	})

	t.Run("MSet and MGet with struct values", func(t *testing.T) {
		type User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		items := map[string]any{
			"user:10": User{ID: 10, Name: "user10"},
			"user:20": User{ID: 20, Name: "user20"},
		}

		err := cache.MSet(ctx, items, time.Minute)
		require.NoError(t, err)

		keys := []string{"user:10", "user:20"}
		var results []User
		err = cache.MGet(ctx, keys, &results)
		require.NoError(t, err)
		require.Len(t, results, 2)
		require.Equal(t, 10, results[0].ID)
		require.Equal(t, "user10", results[0].Name)
		require.Equal(t, 20, results[1].ID)
		require.Equal(t, "user20", results[1].Name)
	})

	t.Run("MSet and MGet with pointer slice", func(t *testing.T) {
		type User struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}

		items := map[string]any{
			"user:30": User{ID: 30, Name: "user30"},
			"user:40": User{ID: 40, Name: "user40"},
		}

		err := cache.MSet(ctx, items, time.Minute)
		require.NoError(t, err)

		keys := []string{"user:30", "user:40"}
		var results []*User
		err = cache.MGet(ctx, keys, &results)
		require.NoError(t, err)
		require.Len(t, results, 2)
		require.Equal(t, 30, results[0].ID)
		require.Equal(t, "user30", results[0].Name)
		require.Equal(t, 40, results[1].ID)
		require.Equal(t, "user40", results[1].Name)
	})

	t.Run("MSet overwrite existing keys", func(t *testing.T) {
		// 先设置
		err := cache.Set(ctx, "user:50", map[string]string{"name": "original"}, time.Minute)
		require.NoError(t, err)

		// MSet 覆盖
		items := map[string]any{
			"user:50": map[string]string{"name": "updated"},
		}
		err = cache.MSet(ctx, items, time.Minute)
		require.NoError(t, err)

		var got map[string]string
		err = cache.Get(ctx, "user:50", &got)
		require.NoError(t, err)
		require.Equal(t, "updated", got["name"])
	})
}
