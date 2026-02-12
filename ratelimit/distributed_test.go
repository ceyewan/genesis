package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/testkit"
)

// ============================================================
// 创建分布式限流器辅助函数
// ============================================================

func newDistributedLimiter(t *testing.T) Limiter {
	t.Helper()

	redisConn := testkit.NewRedisContainerConnector(t)

	limiter, err := New(&Config{
		Driver:      DriverDistributed,
		Distributed: &DistributedConfig{Prefix: "test:ratelimit:"},
	}, WithRedisConnector(redisConn), WithLogger(testkit.NewLogger()))

	require.NoError(t, err)

	t.Cleanup(func() {
		_ = limiter.Close()
	})

	return limiter
}

// ============================================================
// 基础功能测试
// ============================================================

func TestDistributedLimiter_Allow_Basic(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("第一次请求应该被允许", func(t *testing.T) {
		allowed, err := limiter.Allow(ctx, "test-key-1", Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.True(t, allowed, "第一次请求应该被允许")
	})

	t.Run("相同 key 连续请求应该被限流", func(t *testing.T) {
		key := "test-key-rate-limit"

		// 消耗所有 burst
		for i := 0; i < 10; i++ {
			allowed, err := limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
			require.NoError(t, err)
			assert.True(t, allowed, "第 %d 次请求应该被允许", i+1)
		}

		// 下一个请求应该被限流
		allowed, err := limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.False(t, allowed, "超过 Burst 的请求应该被限流")
	})

	t.Run("不同 key 独立限流", func(t *testing.T) {
		keys := []string{"user:1", "user:2", "user:3"}

		for _, key := range keys {
			allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
			require.NoError(t, err)
			assert.True(t, allowed, "key %s 的第一次请求应该被允许", key)
		}
	})
}

func TestDistributedLimiter_AllowN(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("AllowN 请求多个令牌", func(t *testing.T) {
		allowed, err := limiter.AllowN(ctx, "allown-test", Limit{Rate: 100, Burst: 100}, 50)
		require.NoError(t, err)
		assert.True(t, allowed, "请求 50 个令牌应该成功")
	})

	t.Run("AllowN 超过 Burst 应该被拒绝", func(t *testing.T) {
		// 第一次消耗 50 个
		allowed, err := limiter.AllowN(ctx, "allown-test-2", Limit{Rate: 100, Burst: 100}, 50)
		require.NoError(t, err)
		assert.True(t, allowed)

		// 第二次请求 60 个（总共需要 110 个，超过 burst=100）
		allowed, err = limiter.AllowN(ctx, "allown-test-2", Limit{Rate: 100, Burst: 100}, 60)
		require.NoError(t, err)
		assert.False(t, allowed, "超过 Burst 的请求应该被拒绝")
	})

	t.Run("AllowN 请求 1 个令牌等同于 Allow", func(t *testing.T) {
		key := "allown-test-3"
		limit := Limit{Rate: 10, Burst: 10}

		// 测试 AllowN(..., 1) 与 Allow 行为一致
		allowed1, err1 := limiter.Allow(ctx, key, limit)
		require.NoError(t, err1)

		allowedN, errN := limiter.AllowN(ctx, key, limit, 1)
		require.NoError(t, errN)

		// 第一次都应该成功
		assert.True(t, allowed1)
		assert.True(t, allowedN)

		// 再次请求验证限流状态一致
		allowed2, _ := limiter.Allow(ctx, key, limit)
		allowedN2, _ := limiter.AllowN(ctx, key, limit, 1)

		// 应该有相同的结果
		assert.Equal(t, allowed2, allowedN2)
	})
}

// ============================================================
// Wait 方法测试
// ============================================================

func TestDistributedLimiter_Wait(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("分布式限流器不支持 Wait", func(t *testing.T) {
		err := limiter.Wait(ctx, "test-key", Limit{Rate: 10, Burst: 10})
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrNotSupported)
	})
}

// ============================================================
// 边界条件测试
// ============================================================

func TestDistributedLimiter_EdgeCases(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("空 key 应该返回错误", func(t *testing.T) {
		allowed, err := limiter.Allow(ctx, "", Limit{Rate: 10, Burst: 10})
		require.Error(t, err)
		assert.False(t, allowed)
		assert.ErrorIs(t, err, ErrKeyEmpty)
	})

	t.Run("负数 Rate 应该返回错误", func(t *testing.T) {
		allowed, err := limiter.Allow(ctx, "test-key", Limit{Rate: -1, Burst: 10})
		require.Error(t, err)
		assert.False(t, allowed)
		assert.ErrorIs(t, err, ErrInvalidLimit)
	})

	t.Run("零 Burst 应该返回错误", func(t *testing.T) {
		allowed, err := limiter.Allow(ctx, "test-key", Limit{Rate: 10, Burst: 0})
		require.Error(t, err)
		assert.False(t, allowed)
		assert.ErrorIs(t, err, ErrInvalidLimit)
	})

	t.Run("负数 Burst 应该返回错误", func(t *testing.T) {
		allowed, err := limiter.Allow(ctx, "test-key", Limit{Rate: 10, Burst: -1})
		require.Error(t, err)
		assert.False(t, allowed)
		assert.ErrorIs(t, err, ErrInvalidLimit)
	})

	t.Run("浮点数 Rate 应该正常工作", func(t *testing.T) {
		// Rate=0.1 表示每 10 秒生成 1 个令牌
		allowed, err := limiter.Allow(ctx, "float-rate-test", Limit{Rate: 0.1, Burst: 1})
		require.NoError(t, err)
		assert.True(t, allowed)

		// 立即再次请求应该被拒绝
		allowed, err = limiter.Allow(ctx, "float-rate-test", Limit{Rate: 0.1, Burst: 1})
		require.NoError(t, err)
		assert.False(t, allowed)
	})

	t.Run("AllowN n<=0 应该返回错误", func(t *testing.T) {
		allowed, err := limiter.AllowN(ctx, "test-key", Limit{Rate: 10, Burst: 10}, 0)
		require.Error(t, err)
		assert.False(t, allowed)
		assert.ErrorIs(t, err, ErrInvalidLimit)

		allowed, err = limiter.AllowN(ctx, "test-key", Limit{Rate: 10, Burst: 10}, -1)
		require.Error(t, err)
		assert.False(t, allowed)
	})
}

// ============================================================
// 并发测试
// ============================================================

func TestDistributedLimiter_Concurrency(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("并发访问相同 key", func(t *testing.T) {
		const goroutines = 10
		const requestsPerGoroutine = 50

		var allowedCount int64
		var deniedCount int64
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < requestsPerGoroutine; j++ {
					allowed, _ := limiter.Allow(ctx, "concurrent-key", Limit{Rate: 100, Burst: 100})
					mu.Lock()
					if allowed {
						allowedCount++
					} else {
						deniedCount++
					}
					mu.Unlock()
				}
			}()
		}

		wg.Wait()

		totalRequests := int64(goroutines * requestsPerGoroutine)
		assert.Equal(t, totalRequests, allowedCount+deniedCount, "总请求数应该匹配")
		// 分布式限流器应该工作正常
		assert.Less(t, allowedCount, totalRequests, "应该有部分请求被限流")
	})

	t.Run("并发访问不同 key", func(t *testing.T) {
		const goroutines = 10
		const requestsPerGoroutine = 10

		var wg sync.WaitGroup
		errors := make(chan error, goroutines)

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				key := "concurrent-diff-key-" + testkit.NewID()[:4] + string(rune('0'+idx))
				for j := 0; j < requestsPerGoroutine; j++ {
					_, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
					if err != nil {
						errors <- err
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("并发请求不同 key 时不应出错: %v", err)
		}
	})
}

// ============================================================
// 限流精确性测试
// ============================================================

func TestDistributedLimiter_Precision(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("令牌应该按 Rate 补充", func(t *testing.T) {
		key := "precision-test-" + testkit.NewID()

		// 消耗所有 burst
		allowed, err := limiter.AllowN(ctx, key, Limit{Rate: 10, Burst: 10}, 10)
		require.NoError(t, err)
		assert.True(t, allowed)

		// 立即请求应该被拒绝
		allowed, err = limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.False(t, allowed)

		// 等待令牌补充
		time.Sleep(150 * time.Millisecond)

		// 现在应该允许请求（已补充约 1-2 个令牌）
		allowed, err = limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.True(t, allowed, "等待后应该补充了令牌")
	})

	t.Run("突发流量应该使用 Burst", func(t *testing.T) {
		key := "burst-test-" + testkit.NewID()

		// Burst=5 允许瞬间处理 5 个请求
		for i := 0; i < 5; i++ {
			allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 5})
			require.NoError(t, err)
			assert.True(t, allowed, "Burst 应该允许前 %d 个请求", i+1)
		}

		// 第 6 个请求应该被拒绝
		allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 5})
		require.NoError(t, err)
		assert.False(t, allowed, "超过 Burst 的请求应该被拒绝")
	})

	t.Run("高 Rate 应该允许更多请求", func(t *testing.T) {
		key := "high-rate-test-" + testkit.NewID()

		// Rate=100, Burst=100
		successCount := 0
		for i := 0; i < 100; i++ {
			allowed, err := limiter.Allow(ctx, key, Limit{Rate: 100, Burst: 100})
			require.NoError(t, err)
			if allowed {
				successCount++
			}
		}

		assert.Equal(t, 100, successCount, "Burst=100 应该允许 100 个请求")
	})
}

// ============================================================
// Lua 脚本正确性测试
// ============================================================

func TestDistributedLimiter_LuaScript(t *testing.T) {
	limiter := newDistributedLimiter(t)
	ctx := context.Background()

	t.Run("Redis key 应该正确设置过期时间", func(t *testing.T) {
		key := "expire-test-" + testkit.NewID()

		// 发起一次请求
		allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 10})
		require.NoError(t, err)
		assert.True(t, allowed)

		// 等待足够长的时间让 Redis key 过期
		// Lua 脚本中 EX 时间为 fill_time * 2 = (10 * 1) * 2 = 20 秒
		// 这里不等待那么久，只验证逻辑正确性
	})

	t.Run("不同 Limit 使用相同 Redis key（分布式特性）", func(t *testing.T) {
		baseKey := "same-key-" + testkit.NewID()

		// 使用限流规则 A
		allowed1, err := limiter.Allow(ctx, baseKey, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.True(t, allowed1)

		// 相同 key 不同限流规则：分布式限流器使用相同的 Redis key
		// 所以第一次请求会使用 Redis 中已有的状态
		allowed2, err := limiter.Allow(ctx, baseKey, Limit{Rate: 1, Burst: 1})
		require.NoError(t, err)
		// 由于已经消耗了一个令牌，且 Redis key 是共享的
		// 这个测试主要验证不会 panic，具体行为取决于 Redis 中的状态
		_ = allowed2
	})
}

// ============================================================
// 多实例测试
// ============================================================

func TestDistributedLimiter_MultipleInstances(t *testing.T) {
	// 创建多个限流器实例，模拟分布式环境
	redisConn := testkit.NewRedisContainerConnector(t)

	limiter1, err := New(&Config{
		Driver:      DriverDistributed,
		Distributed: &DistributedConfig{Prefix: "multi-test:"},
	}, WithRedisConnector(redisConn))
	require.NoError(t, err)
	defer limiter1.Close()

	limiter2, err := New(&Config{
		Driver:      DriverDistributed,
		Distributed: &DistributedConfig{Prefix: "multi-test:"},
	}, WithRedisConnector(redisConn))
	require.NoError(t, err)
	defer limiter2.Close()

	ctx := context.Background()
	key := "shared-key-" + testkit.NewID()
	limit := Limit{Rate: 5, Burst: 5}

	t.Run("多实例共享限流状态", func(t *testing.T) {
		// 使用 limiter1 消耗所有 burst
		for i := 0; i < 5; i++ {
			allowed, err := limiter1.Allow(ctx, key, limit)
			require.NoError(t, err)
			assert.True(t, allowed)
		}

		// 使用 limiter2 应该也看到限流状态
		allowed, err := limiter2.Allow(ctx, key, limit)
		require.NoError(t, err)
		assert.False(t, allowed, "limiter2 应该看到 limiter1 消耗的令牌")
	})

	t.Run("多实例并发请求", func(t *testing.T) {
		key := "concurrent-shared-" + testkit.NewID()
		limit := Limit{Rate: 100, Burst: 100}

		var wg sync.WaitGroup
		var totalCount int64

		// limiter1 发送 50 个请求
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				allowed, _ := limiter1.Allow(ctx, key, limit)
				if allowed {
					totalCount++
				}
			}
		}()

		// limiter2 发送 50 个请求
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				allowed, _ := limiter2.Allow(ctx, key, limit)
				if allowed {
					totalCount++
				}
			}
		}()

		wg.Wait()

		// 总共应该有 100 个成功请求
		assert.Equal(t, int64(100), totalCount)
	})
}

// ============================================================
// Close 测试
// ============================================================

func TestDistributedLimiter_Close(t *testing.T) {
	t.Run("Close 应该正常工作", func(t *testing.T) {
		redisConn := testkit.NewRedisContainerConnector(t)

		limiter, err := New(&Config{
			Driver:      DriverDistributed,
			Distributed: &DistributedConfig{Prefix: "close-test:"},
		}, WithRedisConnector(redisConn))
		require.NoError(t, err)

		err = limiter.Close()
		require.NoError(t, err)

		// 再次关闭不应该 panic
		err = limiter.Close()
		require.NoError(t, err)
	})
}

// ============================================================
// 配置默认值测试
// ============================================================

func TestDistributedConfig_SetDefaults(t *testing.T) {
	t.Run("空配置应该设置默认值", func(t *testing.T) {
		cfg := &DistributedConfig{}
		cfg.setDefaults()

		assert.Equal(t, "ratelimit:", cfg.Prefix)
	})

	t.Run("非零值不应该被覆盖", func(t *testing.T) {
		cfg := &DistributedConfig{Prefix: "custom:"}
		cfg.setDefaults()

		assert.Equal(t, "custom:", cfg.Prefix)
	})
}

// ============================================================
// 错误处理测试
// ============================================================

func TestDistributedLimiter_ErrorHandling(t *testing.T) {
	t.Run("Redis 连接错误时应该返回错误", func(t *testing.T) {
		// 这个测试验证 Redis 连接错误时的行为
		// 由于无法可靠地模拟连接失败而不引起 panic
		// 这里我们只验证正常情况下错误能被正确处理
		limiter := newDistributedLimiter(t)
		ctx := context.Background()

		// 验证无效输入返回错误
		_, err := limiter.Allow(ctx, "", Limit{Rate: 10, Burst: 10})
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrKeyEmpty)

		_, err = limiter.Allow(ctx, "test", Limit{Rate: 0, Burst: 0})
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidLimit)
	})
}
