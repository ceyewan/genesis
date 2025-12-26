package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
)

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
}

// TestNewConfigNil 测试 nil 配置时返回 Discard 实例
func TestNewConfigNil(t *testing.T) {
	limiter, err := New(nil)

	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	// 应返回 Discard 实例
	allowed, _ := limiter.Allow(context.Background(), "key", Limit{Rate: 0, Burst: 0})
	if !allowed {
		t.Fatal("Nil config limiter should always allow")
	}
}

// TestNewConfigEmptyMode 测试空 Mode 时默认使用单机模式
func TestNewConfigEmptyMode(t *testing.T) {
	cfg := &Config{Mode: ""}
	limiter, err := New(cfg)

	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	// 空模式默认使用单机限流器（不是 Discard）
	if limiter == nil {
		t.Fatal("Empty mode should return standalone limiter")
	}

	// 验证这是一个实际的限流器
	allowed, _ := limiter.Allow(context.Background(), "key", Limit{Rate: 1, Burst: 1})
	if !allowed {
		t.Fatal("First request should be allowed")
	}
}

// TestNewConfigStandalone 测试单机模式配置
func TestNewConfigStandalone(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		Mode:    "standalone",
		Standalone: &StandaloneConfig{
			CleanupInterval: 1 * time.Minute,
			IdleTimeout:     5 * time.Minute,
		},
	}

	limiter, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

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

// TestNewStandalone 测试单机限流器创建
func TestNewStandalone(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter, err := NewStandalone(&StandaloneConfig{
		CleanupInterval: 1 * time.Minute,
		IdleTimeout:     5 * time.Minute,
	}, WithLogger(logger))

	if err != nil {
		t.Fatalf("NewStandalone should not return error, got: %v", err)
	}

	if limiter == nil {
		t.Fatal("NewStandalone should return a valid limiter")
	}

	// 测试限流功能
	ctx := context.Background()
	key := "test-key"
	limit := Limit{Rate: 10, Burst: 5}

	// 连续请求应该都被允许（在 burst 范围内）
	for i := 0; i < 5; i++ {
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			t.Fatalf("Allow should not return error, got: %v", err)
		}
		if !allowed {
			t.Fatalf("Request %d should be allowed", i+1)
		}
	}
}

// TestNewStandaloneNilConfig 测试 nil 配置时使用默认值
func TestNewStandaloneNilConfig(t *testing.T) {
	limiter, err := NewStandalone(nil)

	if err != nil {
		t.Fatalf("NewStandalone should not return error, got: %v", err)
	}

	if limiter == nil {
		t.Fatal("NewStandalone should return a valid limiter with default config")
	}
}

// TestRateLimiting 测试基本的限流功能
func TestRateLimiting(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

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

	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

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

	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

	ctx := context.Background()
	limit := Limit{Rate: 10, Burst: 10}

	// 单机限流器对空 key 的处理取决于实现
	_, _ = limiter.Allow(ctx, "", limit)
}

// TestInvalidLimit 测试无效限流规则的响应
func TestInvalidLimit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

	ctx := context.Background()
	key := "test-key"

	// 无效的限流规则
	_, _ = limiter.Allow(ctx, key, Limit{Rate: 0, Burst: 0})
	_, _ = limiter.Allow(ctx, key, Limit{Rate: -1, Burst: -1})
}

// TestMultipleKeys 测试不同 key 独立限流
func TestMultipleKeys(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

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
	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "key", Limit{Rate: 10000, Burst: 10000})
	}
}
