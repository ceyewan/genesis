package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/stretchr/testify/require"
)

// ============================================================
// Discard 模式测试
// ============================================================

func TestDiscard(t *testing.T) {
	limiter := Discard()

	// Discard 应该始终返回 true
	allowed, err := limiter.Allow(context.Background(), "any-key", Limit{Rate: -1, Burst: -1})
	require.NoError(t, err)
	require.True(t, allowed, "Discard limiter should always allow")

	// AllowN 也应该始终返回 true
	allowed, err = limiter.AllowN(context.Background(), "any-key", Limit{Rate: 0, Burst: 0}, 1000)
	require.NoError(t, err)
	require.True(t, allowed, "Discard limiter should always allow")

	require.NoError(t, limiter.Wait(context.Background(), "any-key", Limit{Rate: 1, Burst: 1}))
	require.NoError(t, limiter.Close())
}

// ============================================================
// New 函数配置测试
// ============================================================

func TestNew_ConfigNil(t *testing.T) {
	limiter, err := New(nil)

	require.Error(t, err, "New should return error for nil config")
	require.Nil(t, limiter, "Limiter should be nil when config is nil")
}

func TestNew_MissingDriver(t *testing.T) {
	cfg := &Config{}
	limiter, err := New(cfg)

	require.Error(t, err, "New should return error when driver is missing")
	require.Nil(t, limiter, "Limiter should be nil when driver is missing")
}

func TestNew_UnsupportedDriver(t *testing.T) {
	cfg := &Config{Driver: DriverType("unknown")}
	limiter, err := New(cfg)

	require.Error(t, err)
	require.Nil(t, limiter)
}

func TestNew_ConfigStandalone(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	cfg := &Config{
		Driver: DriverStandalone,
		Standalone: &StandaloneConfig{
			CleanupInterval: 1 * time.Minute,
			IdleTimeout:     5 * time.Minute,
		},
	}

	limiter, err := New(cfg, WithLogger(logger))
	require.NoError(t, err)
	require.NotNil(t, limiter)
	defer limiter.Close()
}

func TestNew_ConfigDistributed(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	// 分布式模式需要 Redis 连接器，这里只测试缺少连接器的情况
	cfg := &Config{
		Driver:      DriverDistributed,
		Distributed: &DistributedConfig{},
	}

	limiter, err := New(cfg, WithLogger(logger))
	require.Error(t, err)
	require.Nil(t, limiter)
}

// ============================================================
// Driver 常量测试
// ============================================================

func TestDriverConstants(t *testing.T) {
	require.Equal(t, DriverType("standalone"), DriverStandalone)
	require.Equal(t, DriverType("distributed"), DriverDistributed)
}

// ============================================================
// Limit 结构体测试
// ============================================================

func TestLimit_Struct(t *testing.T) {
	limit := Limit{Rate: 10.5, Burst: 20}

	require.Equal(t, float64(10.5), limit.Rate)
	require.Equal(t, 20, limit.Burst)
}

// ============================================================
// 基准测试
// ============================================================

func BenchmarkDiscard(b *testing.B) {
	limiter := Discard()
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "key", Limit{Rate: 1000, Burst: 1000})
	}
}

func BenchmarkStandaloneLimiter(b *testing.B) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	limiter, err := New(&Config{
		Driver:     DriverStandalone,
		Standalone: &StandaloneConfig{},
	}, WithLogger(logger))
	require.NoError(b, err)
	b.Cleanup(func() {
		_ = limiter.Close()
	})

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(ctx, "key", Limit{Rate: 10000, Burst: 10000})
	}
}
