package idgen

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

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

func setupRedisForBench(b *testing.B) connector.RedisConnector {
	logger, _ := clog.New(&clog.Config{
		Level:  "warn",
		Format: "json",
		Output: "stderr",
	})

	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         getEnvOrDefaultBench("REDIS_ADDR", "127.0.0.1:6379"),
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           getEnvIntOrDefaultBench("REDIS_DB", 2),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
		PoolSize:     10,
	}, connector.WithLogger(logger))
	if err != nil {
		b.Skip("Redis not available")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisConn.Connect(ctx); err != nil {
		b.Skip("Redis connection failed")
		redisConn.Close()
		return nil
	}

	b.Cleanup(func() { redisConn.Close() })
	return redisConn
}

func getEnvOrDefaultBench(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvIntOrDefaultBench(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func BenchmarkSequencer_Next_Single(b *testing.B) {
	redis := setupRedisForBench(b)
	if redis == nil {
		return
	}

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
	if redis == nil {
		return
	}

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
	if redis == nil {
		return
	}

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
	if redis == nil {
		return
	}

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
	if redis == nil {
		return
	}

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

