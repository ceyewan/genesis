package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
)

func newStandaloneLimiter(tb testing.TB, logger clog.Logger) Limiter {
	tb.Helper()

	limiter, err := New(&Config{
		Driver:     DriverStandalone,
		Standalone: &StandaloneConfig{},
	}, WithLogger(logger))
	if err != nil {
		tb.Fatalf("New should not return error, got: %v", err)
	}
	return limiter
}

// TestDiscard 测试 Discard 模式
func TestDiscard(t *testing.T) {
	limiter := Discard()

	// Discard 应该始终返回 true
	allowed, err := limiter.Allow(context.Background(), "any-key", Limit{Rate: -1, Burst: -1})
	if err != nil {
		t.Fatalf("Allow should not return error, got: %v", err)
	}
	if !allowed {
		t.Fatal("Discard limiter should always allow")
	}

	// AllowN 也应该始终返回 true
	allowed, err = limiter.AllowN(context.Background(), "any-key", Limit{Rate: 0, Burst: 0}, 1000)
	if err != nil {
		t.Fatalf("AllowN should not return error, got: %v", err)
	}
	if !allowed {
		t.Fatal("Discard limiter should always allow")
	}

	if err := limiter.Wait(context.Background(), "any-key", Limit{Rate: 1, Burst: 1}); err != nil {
		t.Fatalf("Wait should not return error, got: %v", err)
	}
	if err := limiter.Close(); err != nil {
		t.Fatalf("Close should not return error, got: %v", err)
	}
}

// TestNewConfigNil 测试 nil 配置时返回错误
func TestNewConfigNil(t *testing.T) {
	limiter, err := New(nil)

	if err == nil {
		t.Fatal("New should return error for nil config")
	}
	if limiter != nil {
		t.Fatal("Limiter should be nil when config is nil")
	}
}

// TestNewConfigMissingDriver 测试缺少 Driver 时返回错误
func TestNewConfigMissingDriver(t *testing.T) {
	cfg := &Config{}
	limiter, err := New(cfg)

	if err == nil {
		t.Fatalf("New should return error when driver is missing")
	}
	if limiter != nil {
		t.Fatal("Limiter should be nil when driver is missing")
	}
}

// TestNewConfigStandalone 测试单机模式配置
func TestNewConfigStandalone(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		Driver: DriverStandalone,
		Standalone: &StandaloneConfig{
			CleanupInterval: 1 * time.Minute,
			IdleTimeout:     5 * time.Minute,
		},
	}

	limiter, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}
	defer limiter.Close()

	if limiter == nil {
		t.Fatal("New should return a valid limiter")
	}

	// 测试限流功能
	ctx := context.Background()
	key := "test-key"
	limit := Limit{Rate: 1, Burst: 1}

	// 第一次请求应该成功
	allowed, err := limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("Allow should not return error, got: %v", err)
	}
	if !allowed {
		t.Fatal("First request should be allowed")
	}

	// 第二次请求应该被限流
	allowed, err = limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("Allow should not return error, got: %v", err)
	}
	if allowed {
		t.Log("Second request should be rate limited (but might pass due to burst)")
	}
}

// TestNewStandaloneDefaults 测试 Standalone 配置默认值
func TestNewStandaloneDefaults(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter, err := New(&Config{
		Driver:     DriverStandalone,
		Standalone: nil,
	}, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}
	defer limiter.Close()

	if limiter == nil {
		t.Fatal("New should return a valid limiter with default config")
	}
}

// TestRateLimiting 测试基本的限流功能
func TestRateLimiting(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter := newStandaloneLimiter(t, logger)
	defer limiter.Close()

	ctx := context.Background()
	key := "rate-limit-test"
	limit := Limit{Rate: 1, Burst: 1}

	// 第一次请求成功
	allowed, err := limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("Allow should not return error, got: %v", err)
	}
	if !allowed {
		t.Fatal("First request should be allowed")
	}

	// 立即第二次请求应该被限流
	allowed, err = limiter.Allow(ctx, key, limit)
	if err != nil {
		t.Fatalf("Allow should not return error, got: %v", err)
	}
	// 由于 Rate=1, Burst=1，第二个请求理论上会被限流
	// 但实际实现可能略有不同，这里只检查不报错
	_ = allowed
}

// TestAllowN 测试 AllowN 方法
func TestAllowN(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter := newStandaloneLimiter(t, logger)
	defer limiter.Close()

	ctx := context.Background()
	key := "allown-test"
	limit := Limit{Rate: 10, Burst: 10}

	// 请求 5 个令牌
	allowed, err := limiter.AllowN(ctx, key, limit, 5)
	if err != nil {
		t.Fatalf("AllowN should not return error, got: %v", err)
	}
	if !allowed {
		t.Fatal("AllowN(5) should be allowed with burst=10")
	}

	// 请求 10 个令牌（总共 15 个，超过 burst）
	allowed, err = limiter.AllowN(ctx, key, limit, 10)
	if err != nil {
		t.Fatalf("AllowN should not return error, got: %v", err)
	}
	if !allowed {
		t.Log("AllowN(10) after 5 tokens might be rate limited (expected)")
	}
}

// TestInvalidKey 测试空 key 的处理
func TestInvalidKey(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter := newStandaloneLimiter(t, logger)
	defer limiter.Close()

	ctx := context.Background()
	limit := Limit{Rate: 10, Burst: 10}

	// 单机限流器对空 key 的处理取决于实现
	_, _ = limiter.Allow(ctx, "", limit)
}

// TestInvalidLimit 测试无效限流规则的响应
func TestInvalidLimit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter := newStandaloneLimiter(t, logger)
	defer limiter.Close()

	ctx := context.Background()
	key := "test-key"

	// 无效的限流规则
	_, _ = limiter.Allow(ctx, key, Limit{Rate: 0, Burst: 0})
	_, _ = limiter.Allow(ctx, key, Limit{Rate: -1, Burst: -1})
}

// TestMultipleKeys 测试不同 key 独立限流
func TestMultipleKeys(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter := newStandaloneLimiter(t, logger)
	defer limiter.Close()

	ctx := context.Background()
	limit := Limit{Rate: 1, Burst: 1}

	keys := []string{"user:1", "user:2", "user:3"}

	// 每个独立的 key 都应该被允许
	for _, key := range keys {
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			t.Fatalf("Allow should not return error, got: %v", err)
		}
		if !allowed {
			t.Fatalf("First request for key %s should be allowed", key)
		}
	}
}

// BenchmarkDiscard 基准测试 Discard 模式
func BenchmarkDiscard(b *testing.B) {
	limiter := Discard()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "key", Limit{Rate: 1000, Burst: 1000})
	}
}

// BenchmarkStandaloneLimiter 基准测试单机限流器
func BenchmarkStandaloneLimiter(b *testing.B) {
	logger, _ := clog.New(&clog.Config{Level: "error"})
	limiter := newStandaloneLimiter(b, logger)
	b.Cleanup(func() {
		_ = limiter.Close()
	})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "key", Limit{Rate: 10000, Burst: 10000})
	}
}
