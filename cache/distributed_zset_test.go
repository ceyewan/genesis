package cache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDistributed_ZSet_Integration 测试有序集合操作
func TestDistributed_ZSet_Integration(t *testing.T) {
	cache := setupTestDistributed(t, "test:dist:zset:")
	ctx := context.Background()

	t.Run("ZAdd and ZScore", func(t *testing.T) {
		member := map[string]string{"id": "1", "name": "alice"}
		err := cache.ZAdd(ctx, "zset:1", 100.5, member)
		require.NoError(t, err)

		score, err := cache.ZScore(ctx, "zset:1", member)
		require.NoError(t, err)
		require.Equal(t, 100.5, score)
	})

	t.Run("ZScore non-existent member returns ErrMiss", func(t *testing.T) {
		member := map[string]string{"id": "nonexistent"}
		score, err := cache.ZScore(ctx, "zset:1", member)
		require.ErrorIs(t, err, ErrMiss)
		require.Equal(t, float64(0), score)
	})

	t.Run("ZRange", func(t *testing.T) {
		// 添加多个成员
		for i := 1; i <= 5; i++ {
			member := map[string]int{"rank": i}
			err := cache.ZAdd(ctx, "zset:2", float64(i*10), member)
			require.NoError(t, err)
		}

		// 获取前 3 名
		var result []map[string]int
		err := cache.ZRange(ctx, "zset:2", 0, 2, &result)
		require.NoError(t, err)
		require.Len(t, result, 3)
		require.Equal(t, 1, result[0]["rank"])
		require.Equal(t, 2, result[1]["rank"])
		require.Equal(t, 3, result[2]["rank"])
	})

	t.Run("ZRevRange", func(t *testing.T) {
		// 获取倒数 3 名
		var result []map[string]int
		err := cache.ZRevRange(ctx, "zset:2", 0, 2, &result)
		require.NoError(t, err)
		require.Len(t, result, 3)
		require.Equal(t, 5, result[0]["rank"])
		require.Equal(t, 4, result[1]["rank"])
		require.Equal(t, 3, result[2]["rank"])
	})

	t.Run("ZRangeByScore", func(t *testing.T) {
		var result []map[string]int
		err := cache.ZRangeByScore(ctx, "zset:2", 20, 35, &result)
		require.NoError(t, err)
		require.Len(t, result, 2)
		require.Equal(t, 2, result[0]["rank"])
		require.Equal(t, 3, result[1]["rank"])
	})

	t.Run("ZRem", func(t *testing.T) {
		member := map[string]string{"id": "to_remove"}
		err := cache.ZAdd(ctx, "zset:3", 50.0, member)
		require.NoError(t, err)

		// 验证已添加
		score, err := cache.ZScore(ctx, "zset:3", member)
		require.NoError(t, err)
		require.Equal(t, 50.0, score)

		// 删除
		err = cache.ZRem(ctx, "zset:3", member)
		require.NoError(t, err)

		// 验证已删除
		_, err = cache.ZScore(ctx, "zset:3", member)
		require.ErrorIs(t, err, ErrMiss)
	})

	t.Run("ZRem with multiple members", func(t *testing.T) {
		member1 := map[string]string{"id": "1"}
		member2 := map[string]string{"id": "2"}
		member3 := map[string]string{"id": "3"}

		err := cache.ZAdd(ctx, "zset:4", 10.0, member1)
		require.NoError(t, err)
		err = cache.ZAdd(ctx, "zset:4", 20.0, member2)
		require.NoError(t, err)
		err = cache.ZAdd(ctx, "zset:4", 30.0, member3)
		require.NoError(t, err)

		// 删除多个
		err = cache.ZRem(ctx, "zset:4", member1, member2)
		require.NoError(t, err)

		// 验证
		_, err = cache.ZScore(ctx, "zset:4", member1)
		require.ErrorIs(t, err, ErrMiss)
		_, err = cache.ZScore(ctx, "zset:4", member2)
		require.ErrorIs(t, err, ErrMiss)
		_, err = cache.ZScore(ctx, "zset:4", member3)
		require.NoError(t, err)
	})

	t.Run("ZRange with simple string members", func(t *testing.T) {
		err := cache.ZAdd(ctx, "zset:5", 100.0, "member1")
		require.NoError(t, err)
		err = cache.ZAdd(ctx, "zset:5", 200.0, "member2")
		require.NoError(t, err)

		var result []string
		err = cache.ZRange(ctx, "zset:5", 0, -1, &result)
		require.NoError(t, err)
		require.Len(t, result, 2)
		require.Contains(t, result, "member1")
		require.Contains(t, result, "member2")
	})

	t.Run("Leaderboard scenario", func(t *testing.T) {
		// 模拟排行榜场景
		players := []struct {
			ID    int
			Score float64
		}{
			{ID: 1, Score: 1500},
			{ID: 2, Score: 2000},
			{ID: 3, Score: 1800},
			{ID: 4, Score: 2200},
			{ID: 5, Score: 1600},
		}

		for _, p := range players {
			member := map[string]int{"player_id": p.ID}
			err := cache.ZAdd(ctx, "leaderboard", p.Score, member)
			require.NoError(t, err)
		}

		// 获取前 3 名（分数从高到低）
		var top3 []map[string]int
		err := cache.ZRevRange(ctx, "leaderboard", 0, 2, &top3)
		require.NoError(t, err)
		require.Len(t, top3, 3)
		require.Equal(t, 4, top3[0]["player_id"]) // 2200
		require.Equal(t, 2, top3[1]["player_id"]) // 2000
		require.Equal(t, 3, top3[2]["player_id"]) // 1800
	})
}
