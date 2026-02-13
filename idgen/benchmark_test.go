package idgen

import (
	"context"
	"strconv"
	"testing"
	"time"

	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// ========================================
// Snowflake Benchmark
// ========================================

func BenchmarkSnowflake_Next(b *testing.B) {
	gen, _ := NewGenerator(&GeneratorConfig{WorkerID: 1})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gen.Next()
	}
}

func BenchmarkSnowflake_Next_Parallel(b *testing.B) {
	gen, _ := NewGenerator(&GeneratorConfig{WorkerID: 1})
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			gen.Next()
		}
	})
}

// ========================================
// UUID Benchmark
// ========================================

func BenchmarkUUID(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UUID()
	}
}

func BenchmarkUUID_Parallel(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			UUID()
		}
	})
}

// ========================================
// Sequencer Benchmark (需要 Redis)
// ========================================

// setupRedisForBench 创建 Redis 连接用于基准测试
// 注意：使用 testcontainers 会为每个 benchmark 创建新容器，开销较大
// 如需更高性能的 benchmark，建议使用本地 Redis
func setupRedisForBench(b *testing.B) connector.RedisConnector {
	ctx := context.Background()

	container, err := rediscontainer.Run(ctx, "redis:7.2-alpine")
	if err != nil {
		b.Fatal("Failed to start Redis container:", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		b.Fatal("Failed to get container host:", err)
	}

	mappedPort, err := container.MappedPort(ctx, "6379")
	if err != nil {
		b.Fatal("Failed to get container port:", err)
	}

	// 注册 cleanup
	b.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	cfg := &connector.RedisConfig{
		Name:         "bench-redis",
		Addr:         host + ":" + mappedPort.Port(),
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	logger, _ := clog.New(&clog.Config{
		Level:  "warn",
		Format: "json",
		Output: "stderr",
	})

	conn, err := connector.NewRedis(cfg, connector.WithLogger(logger))
	if err != nil {
		b.Fatal("Failed to create redis connector:", err)
	}

	if err := conn.Connect(ctx); err != nil {
		b.Fatal("Failed to connect to redis:", err)
	}

	b.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

func BenchmarkSequencer_Next_Single(b *testing.B) {
	redis := setupRedisForBench(b)

	ctx := context.Background()
	seq, _ := NewSequencer(&SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "bench:seq",
		Step:      1,
	}, WithRedisConnector(redis))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq.Next(ctx, "key1")
	}
}

func BenchmarkSequencer_Next_Parallel(b *testing.B) {
	redis := setupRedisForBench(b)

	ctx := context.Background()
	seq, _ := NewSequencer(&SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "bench:seq",
		Step:      1,
	}, WithRedisConnector(redis))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// 每个 goroutine 使用不同的 key 避免 contention
		key := strconv.Itoa(int(time.Now().UnixNano()) % 100)
		for pb.Next() {
			seq.Next(ctx, key)
		}
	})
}

func BenchmarkSequencer_Next_DifferentKeys(b *testing.B) {
	redis := setupRedisForBench(b)

	ctx := context.Background()
	seq, _ := NewSequencer(&SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "bench:seq",
		Step:      1,
	}, WithRedisConnector(redis))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := strconv.Itoa(i % 100) // 100 个不同的 key
		seq.Next(ctx, key)
	}
}

func BenchmarkSequencer_NextBatch(b *testing.B) {
	redis := setupRedisForBench(b)

	ctx := context.Background()
	seq, _ := NewSequencer(&SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "bench:batch",
		Step:      1,
	}, WithRedisConnector(redis))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seq.NextBatch(ctx, "key1", 10)
	}
}

func BenchmarkSequencer_NextBatch_Parallel(b *testing.B) {
	redis := setupRedisForBench(b)

	ctx := context.Background()
	seq, _ := NewSequencer(&SequencerConfig{
		Driver:    "redis",
		KeyPrefix: "bench:batch",
		Step:      1,
	}, WithRedisConnector(redis))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// 每个 goroutine 使用不同的 key
		key := strconv.Itoa(int(time.Now().UnixNano()) % 100)
		for pb.Next() {
			seq.NextBatch(ctx, key, 10)
		}
	})
}
