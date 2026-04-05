package cache

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// setupTestLocal 创建用于测试的本地缓存实例
func setupTestLocal(t *testing.T, maxEntries int) Local {
	t.Helper()

	logger := clog.Discard()
	cfg := &LocalConfig{
		Driver:     DriverOtter,
		MaxEntries: maxEntries,
		Serializer: "json",
		DefaultTTL: time.Hour,
	}

	local, err := NewLocal(cfg, WithLogger(logger))
	require.NoError(t, err)

	return local
}

// TestLocal_KV_Integration 测试本地缓存的 KV 操作
func TestLocal_KV_Integration(t *testing.T) {
	cache := setupTestLocal(t, 1000)
	defer cache.Close()
	ctx := context.Background()

	t.Run("Set and Get", func(t *testing.T) {
		value := map[string]string{"name": "alice", "city": "NYC"}
		err := cache.Set(ctx, "user:1", value, time.Minute)
		require.NoError(t, err)

		var got map[string]string
		err = cache.Get(ctx, "user:1", &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
		require.Equal(t, "NYC", got["city"])
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

	t.Run("Delete non-existent key", func(t *testing.T) {
		err := cache.Delete(ctx, "nonexistent")
		require.NoError(t, err) // 删除不存在的 key 不应该报错
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
}

// TestLocal_ValueSemantics 测试本地缓存的值语义
func TestLocal_ValueSemantics(t *testing.T) {
	cache := setupTestLocal(t, 1000)
	defer cache.Close()
	ctx := context.Background()

	t.Run("Modify original value does not affect cached value", func(t *testing.T) {
		value := map[string]string{"name": "alice"}
		err := cache.Set(ctx, "key", value, time.Minute)
		require.NoError(t, err)

		// 修改原值
		value["name"] = "bob"

		// 缓存中的值不应该受影响
		var got map[string]string
		err = cache.Get(ctx, "key", &got)
		require.NoError(t, err)
		require.Equal(t, "alice", got["name"])
	})

	t.Run("Modify retrieved value does not affect cached value", func(t *testing.T) {
		err := cache.Set(ctx, "key2", map[string]string{"name": "charlie"}, time.Minute)
		require.NoError(t, err)

		var got1 map[string]string
		err = cache.Get(ctx, "key2", &got1)
		require.NoError(t, err)

		// 修改获取的值
		got1["name"] = "dave"

		// 再次获取，缓存中的值不应该受影响
		var got2 map[string]string
		err = cache.Get(ctx, "key2", &got2)
		require.NoError(t, err)
		require.Equal(t, "charlie", got2["name"])
	})
}

// TestLocal_MaxEntries 测试最大条目数限制
func TestLocal_MaxEntries(t *testing.T) {
	cache := setupTestLocal(t, 10)
	defer cache.Close()
	ctx := context.Background()

	t.Run("Eviction when exceeding MaxEntries", func(t *testing.T) {
		// 写入 15 个条目
		for i := range 15 {
			err := cache.Set(ctx, "key:"+strconv.Itoa(i), i, time.Hour)
			require.NoError(t, err)
		}

		// 验证所有写入的键都存在（otter 可能会异步驱逐）
		// 这里主要验证写入不会失败
		for i := range 15 {
			var got int
			err := cache.Get(ctx, "key:"+strconv.Itoa(i), &got)
			// 可能命中也可能被驱逐，都不应该报错
			if err == nil {
				require.Equal(t, i, got)
			} else {
				require.ErrorIs(t, err, ErrMiss)
			}
		}
	})
}

// TestLocal_Concurrency 测试并发安全性
func TestLocal_Concurrency(t *testing.T) {
	cache := setupTestLocal(t, 10000)
	defer cache.Close()
	ctx := context.Background()

	t.Run("Concurrent Set and Get", func(t *testing.T) {
		const goroutines = 100
		const iterations = 100

		var wg sync.WaitGroup
		errCh := make(chan error, goroutines)
		wg.Add(goroutines)

		for i := range goroutines {
			go func(id int) {
				defer wg.Done()
				for j := range iterations {
					key := "key:" + strconv.Itoa(id) + ":" + strconv.Itoa(j)
					value := id*1000 + j

					err := cache.Set(ctx, key, value, time.Minute)
					if err != nil {
						errCh <- err
						return
					}

					var got int
					err = cache.Get(ctx, key, &got)
					if err != nil {
						errCh <- err
						return
					}
					if got != value {
						errCh <- xerrors.New("unexpected cached value")
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errCh)
		for err := range errCh {
			require.NoError(t, err)
		}
	})

	t.Run("Concurrent Set/Delete/Get on same key", func(t *testing.T) {
		const iterations = 1000

		var wg sync.WaitGroup
		errCh := make(chan error, 2*iterations)
		wg.Add(3)

		// Goroutine 1: Set
		go func() {
			defer wg.Done()
			for i := range iterations {
				err := cache.Set(ctx, "shared:key", i, time.Minute)
				if err != nil {
					errCh <- err
					return
				}
			}
		}()

		// Goroutine 2: Get
		go func() {
			defer wg.Done()
			for range iterations {
				var got int
				err := cache.Get(ctx, "shared:key", &got)
				// 可能命中也可能不命中，但不应崩溃
				if err == nil && got < 0 {
					errCh <- xerrors.New("unexpected negative cached value")
					return
				}
			}
		}()

		// Goroutine 3: Delete
		go func() {
			defer wg.Done()
			for range iterations {
				err := cache.Delete(ctx, "shared:key")
				if err != nil {
					errCh <- err
					return
				}
			}
		}()

		wg.Wait()
		close(errCh)
		for err := range errCh {
			require.NoError(t, err)
		}
	})
}

// TestLocal_Close 测试 Close 操作
func TestLocal_Close(t *testing.T) {
	cache := setupTestLocal(t, 100)
	ctx := context.Background()

	err := cache.Set(ctx, "key", "value", time.Minute)
	require.NoError(t, err)

	err = cache.Close()
	require.NoError(t, err)

	// Close 后操作应该不 panic（但行为未定义）
	err = cache.Set(ctx, "key2", "value2", time.Minute)
	require.NoError(t, err)
}
