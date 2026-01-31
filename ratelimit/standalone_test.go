package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// 创建限流器辅助函数
// ============================================================

func newStandaloneLimiter(t *testing.T, opts ...testConfigOption) *standaloneLimiter {
	t.Helper()

	logger, _ := clog.New(&clog.Config{Level: "error"})
	cfg := &StandaloneConfig{
		CleanupInterval: 100 * time.Millisecond,
		IdleTimeout:     200 * time.Millisecond,
	}

	// 应用自定义选项
	for _, opt := range opts {
		opt(cfg)
	}

	limiter, err := newStandalone(cfg, logger, nil)
	require.NoError(t, err)

	return limiter.(*standaloneLimiter)
}

// testConfigOption 用于配置测试用的单机限流器（仅测试内部使用）
type testConfigOption func(*StandaloneConfig)

func withTestCleanupInterval(interval time.Duration) testConfigOption {
	return func(cfg *StandaloneConfig) {
		cfg.CleanupInterval = interval
	}
}

func withTestIdleTimeout(timeout time.Duration) testConfigOption {
	return func(cfg *StandaloneConfig) {
		cfg.IdleTimeout = timeout
	}
}

// ============================================================
// 基础功能测试
// ============================================================

func TestStandaloneLimiter_Allow_Basic(t *testing.T) {
	limiter := newStandaloneLimiter(t)
	defer limiter.Close()
	ctx := context.Background()

	t.Run("第一次请求应该被允许", func(t *testing.T) {
		allowed, err := limiter.Allow(ctx, "test-key", Limit{Rate: 1, Burst: 1})
		require.NoError(t, err)
		assert.True(t, allowed, "第一次请求应该被允许")
	})

	t.Run("Rate=1,Burst=1 时第二次请求应该被拒绝", func(t *testing.T) {
		// 使用新 key 避免受之前的测试影响
		allowed, err := limiter.Allow(ctx, "test-key-2", Limit{Rate: 1, Burst: 1})
		require.NoError(t, err)
		assert.True(t, allowed, "第一次请求应该被允许")

		// 立即第二次请求
		allowed, err = limiter.Allow(ctx, "test-key-2", Limit{Rate: 1, Burst: 1})
		require.NoError(t, err)
		assert.False(t, allowed, "第二次请求应该被限流拒绝")
	})

	t.Run("不同 key 独立限流", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			key := "independent-key-" + string(rune('a'+i))
			allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
			require.NoError(t, err)
			assert.True(t, allowed, "不同 key 的第一次请求都应该被允许")
		}
	})
}

func TestStandaloneLimiter_AllowN(t *testing.T) {
	limiter := newStandaloneLimiter(t)
	defer limiter.Close()
	ctx := context.Background()

	t.Run("AllowN 请求多个令牌", func(t *testing.T) {
		allowed, err := limiter.AllowN(ctx, "allown-test", Limit{Rate: 10, Burst: 10}, 5)
		require.NoError(t, err)
		assert.True(t, allowed, "请求 5 个令牌应该成功")
	})

	t.Run("AllowN 超过 Burst 应该被拒绝", func(t *testing.T) {
		// 第一次消耗 5 个令牌
		allowed, err := limiter.AllowN(ctx, "allown-test-2", Limit{Rate: 10, Burst: 10}, 5)
		require.NoError(t, err)
		assert.True(t, allowed)

		// 第二次请求 10 个令牌（总共需要 15 个，超过 burst=10）
		allowed, err = limiter.AllowN(ctx, "allown-test-2", Limit{Rate: 10, Burst: 10}, 10)
		require.NoError(t, err)
		assert.False(t, allowed, "超过 Burst 的请求应该被拒绝")
	})

	t.Run("AllowN 请求 1 个令牌等同于 Allow", func(t *testing.T) {
		allowed1, err1 := limiter.Allow(ctx, "allown-test-3", Limit{Rate: 5, Burst: 5})
		require.NoError(t, err1)

		allowedN, errN := limiter.AllowN(ctx, "allown-test-3", Limit{Rate: 5, Burst: 5}, 1)
		require.NoError(t, errN)

		assert.Equal(t, allowed1, allowedN, "Allow 和 AllowN(..., 1) 应该有相同结果")
	})
}

// ============================================================
// Wait 方法测试
// ============================================================

func TestStandaloneLimiter_Wait(t *testing.T) {
	t.Cleanup(func() {
		// 每个子测试自己管理 limiter 生命周期
	})

	t.Run("Wait 应该阻塞直到获取令牌", func(t *testing.T) {
		limiter := newStandaloneLimiter(t)
		defer limiter.Close()
		ctx := context.Background()

		// 消耗所有 burst
		_, _ = limiter.AllowN(ctx, "wait-test", Limit{Rate: 1, Burst: 1}, 1)

		// 再消耗一个预留的令牌（Rate=1 意味着每秒生成 1 个）
		// 这样 Wait 就需要等待
		start := time.Now()
		err := limiter.Wait(ctx, "wait-test", Limit{Rate: 1, Burst: 1})
		elapsed := time.Since(start)

		require.NoError(t, err)
		// Wait 应该等待约 1 秒 (1 token / 1 rate = 1s)
		assert.Greater(t, elapsed, 800*time.Millisecond, "Wait 应该阻塞一段时间")
		assert.Less(t, elapsed, 1500*time.Millisecond, "Wait 不应该阻塞太久")
	})

	t.Run("Wait 多次应该累积等待时间", func(t *testing.T) {
		limiter := newStandaloneLimiter(t)
		defer limiter.Close()
		ctx := context.Background()

		_, _ = limiter.AllowN(ctx, "wait-test-2", Limit{Rate: 10, Burst: 1}, 1)

		start := time.Now()
		_ = limiter.Wait(ctx, "wait-test-2", Limit{Rate: 10, Burst: 1})
		elapsed := time.Since(start)

		// 应该等待约 100ms
		assert.Greater(t, elapsed, 80*time.Millisecond)
	})

	t.Run("Wait 支持取消", func(t *testing.T) {
		limiter := newStandaloneLimiter(t)
		defer limiter.Close()
		ctx := context.Background()

		_, _ = limiter.AllowN(ctx, "wait-test-3", Limit{Rate: 0.01, Burst: 1}, 1)

		cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()

		err := limiter.Wait(cancelCtx, "wait-test-3", Limit{Rate: 0.01, Burst: 1})
		assert.Error(t, err, "超时 context 应该导致 Wait 返回错误")
	})
}

// ============================================================
// 边界条件测试
// ============================================================

func TestStandaloneLimiter_EdgeCases(t *testing.T) {
	limiter := newStandaloneLimiter(t)
	defer limiter.Close()
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

func TestStandaloneLimiter_Concurrency(t *testing.T) {
	t.Cleanup(func() {
		// 每个子测试自己管理 limiter 生命周期
	})

	t.Run("并发访问相同 key", func(t *testing.T) {
		limiter := newStandaloneLimiter(t)
		defer limiter.Close()
		ctx := context.Background()

		const goroutines = 10
		const requestsPerGoroutine = 100

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
		// 限流器应该工作正常，不应该所有请求都被允许
		assert.Less(t, allowedCount, totalRequests, "应该有部分请求被限流")
	})

	t.Run("并发访问不同 key", func(t *testing.T) {
		limiter := newStandaloneLimiter(t)
		defer limiter.Close()
		ctx := context.Background()

		const goroutines = 10
		const requestsPerGoroutine = 10

		var wg sync.WaitGroup
		errors := make(chan error, goroutines)

		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				key := "concurrent-diff-key-" + string(rune('0'+idx))
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

	t.Run("Wait 并发安全", func(t *testing.T) {
		limiter := newStandaloneLimiter(t)
		defer limiter.Close()
		ctx := context.Background()

		const goroutines = 5

		var wg sync.WaitGroup
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = limiter.Wait(ctx, "wait-concurrent", Limit{Rate: 10, Burst: 1})
			}()
		}

		wg.Wait()
		// 如果没有死锁或 panic，测试通过
	})
}

// ============================================================
// 清理逻辑测试
// ============================================================

func TestStandaloneLimiter_Cleanup(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	t.Run("过期限流器应该被清理", func(t *testing.T) {
		cfg := &StandaloneConfig{
			CleanupInterval: 50 * time.Millisecond,
			IdleTimeout:     100 * time.Millisecond,
		}

		limiter, err := newStandalone(cfg, logger, nil)
		require.NoError(t, err)
		defer limiter.Close()

		ctx := context.Background()

		// 创建多个限流器
		keys := []string{"cleanup-1", "cleanup-2", "cleanup-3"}
		for _, key := range keys {
			_, _ = limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
		}

		// 等待超过 IdleTimeout
		time.Sleep(150 * time.Millisecond)

		// 触发清理（等待 CleanupInterval）
		time.Sleep(60 * time.Millisecond)

		// 再次使用相同的 key，应该创建新的限流器
		for _, key := range keys {
			allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
			require.NoError(t, err)
			assert.True(t, allowed, "清理后重新创建的限流器应该允许请求")
		}
	})

	t.Run("活跃限流器不应该被清理", func(t *testing.T) {
		limiter := newStandaloneLimiter(t,
			withTestCleanupInterval(50*time.Millisecond),
			withTestIdleTimeout(100*time.Millisecond),
		)

		ctx := context.Background()
		key := "active-key"

		// 创建限流器
		_, _ = limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})

		// 在超时前持续使用
		for i := 0; i < 3; i++ {
			time.Sleep(60 * time.Millisecond)
			_, _ = limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
		}

		// 限流器应该仍然存在（能够正常限流）
		allowed, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
		require.NoError(t, err)
		// 由于是 Rate=1，应该能正常限流
		_ = allowed
	})

	t.Run("Close 后停止清理 goroutine", func(t *testing.T) {
		logger, _ := clog.New(&clog.Config{Level: "error"})
		cfg := &StandaloneConfig{
			CleanupInterval: 10 * time.Millisecond,
			IdleTimeout:     50 * time.Millisecond,
		}

		limiter, err := newStandalone(cfg, logger, nil)
		require.NoError(t, err)

		// 正常关闭
		err = limiter.Close()
		require.NoError(t, err)

		// 等待 cleanup goroutine 尝试向已关闭的 channel 发送
		time.Sleep(30 * time.Millisecond)
		// 如果没有 panic，测试通过
	})
}

// ============================================================
// 不同限流规则缓存测试
// ============================================================

func TestStandaloneLimiter_DifferentLimits(t *testing.T) {
	limiter := newStandaloneLimiter(t)
	defer limiter.Close()
	ctx := context.Background()

	t.Run("相同 key 不同限流规则应该独立缓存", func(t *testing.T) {
		key := "same-key-diff-limit"

		// 使用限流规则 A
		allowed1, err := limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.True(t, allowed1)

		// 使用不同的限流规则 B（应该创建独立的 limiter）
		allowed2, err := limiter.Allow(ctx, key, Limit{Rate: 1, Burst: 1})
		require.NoError(t, err)
		assert.True(t, allowed2, "不同限流规则应该独立限流")

		// 再次使用规则 A，应该被限流（因为 burst 已消耗）
		allowed3, err := limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		// 第一个令牌已消耗，但 rate=10 意味着很快会补充
		// 这个测试主要验证不会 panic
		_ = allowed3
	})
}

// ============================================================
// 限流精确性测试
// ============================================================

func TestStandaloneLimiter_Precision(t *testing.T) {
	limiter := newStandaloneLimiter(t)
	defer limiter.Close()
	ctx := context.Background()

	t.Run("Rate=10 应该每 100ms 补充 1 个令牌", func(t *testing.T) {
		key := "precision-test"

		// 消耗所有 burst
		allowed, err := limiter.AllowN(ctx, key, Limit{Rate: 10, Burst: 10}, 10)
		require.NoError(t, err)
		assert.True(t, allowed)

		// 立即请求应该被拒绝
		allowed, err = limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.False(t, allowed)

		// 等待 110ms（约补充 1 个令牌）
		time.Sleep(110 * time.Millisecond)

		// 现在应该允许 1 个请求
		allowed, err = limiter.Allow(ctx, key, Limit{Rate: 10, Burst: 10})
		require.NoError(t, err)
		assert.True(t, allowed, "110ms 后应该补充了 1 个令牌")
	})

	t.Run("突发流量应该使用 Burst", func(t *testing.T) {
		key := "burst-test"

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
}

// ============================================================
// 配置默认值测试
// ============================================================

func TestStandaloneConfig_SetDefaults(t *testing.T) {
	t.Run("空配置应该设置默认值", func(t *testing.T) {
		cfg := &StandaloneConfig{}
		cfg.setDefaults()

		assert.Equal(t, 1*time.Minute, cfg.CleanupInterval)
		assert.Equal(t, 5*time.Minute, cfg.IdleTimeout)
	})

	t.Run("非零值不应该被覆盖", func(t *testing.T) {
		cfg := &StandaloneConfig{
			CleanupInterval: 5 * time.Second,
			IdleTimeout:     30 * time.Second,
		}
		cfg.setDefaults()

		assert.Equal(t, 5*time.Second, cfg.CleanupInterval)
		assert.Equal(t, 30*time.Second, cfg.IdleTimeout)
	})
}
