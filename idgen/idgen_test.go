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

// getEnvOrDefault 获取环境变量，如果不存在则返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault 获取环境变量并转换为 int，如果不存在或转换失败则返回默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// setupLogger 设置日志器
func setupLogger(t *testing.T) clog.Logger {
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	return logger
}

// setupRedisConn 设置 Redis 连接
func setupRedisConn(t *testing.T) connector.RedisConnector {
	logger := setupLogger(t)

	redisConn, err := connector.NewRedis(&connector.RedisConfig{
		Addr:         getEnvOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		Password:     getEnvOrDefault("REDIS_PASSWORD", ""),
		DB:           getEnvIntOrDefault("REDIS_DB", 2),
		DialTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
	}, connector.WithLogger(logger))
	if err != nil {
		t.Skipf("Redis not available, skipping tests: %v", err)
		return nil
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisConn.Connect(ctx); err != nil {
		t.Skipf("Failed to connect to Redis, skipping tests: %v", err)
		redisConn.Close()
		return nil
	}

	t.Cleanup(func() {
		redisConn.Close()
	})

	return redisConn
}

func TestNewUUID(t *testing.T) {
	logger := setupLogger(t)

	tests := []struct {
		name        string
		cfg         *UUIDConfig
		opts        []Option
		expectError bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			expectError: true,
		},
		{
			name: "valid config v4 without logger",
			cfg: &UUIDConfig{
				Version: "v4",
			},
			expectError: false,
		},
		{
			name: "valid config v7 with logger",
			cfg: &UUIDConfig{
				Version: "v7",
			},
			opts:        []Option{WithLogger(logger)},
			expectError: false,
		},
		{
			name: "invalid version",
			cfg: &UUIDConfig{
				Version: "v99",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewUUID(tt.cfg, tt.opts...)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if gen == nil {
				t.Error("Expected generator but got nil")
			}
		})
	}
}

func TestUUID_Next(t *testing.T) {
	gen, err := NewUUID(&UUIDConfig{Version: "v4"})
	if err != nil {
		t.Fatalf("Failed to create UUID generator: %v", err)
	}

	t.Run("Generate v4 UUID", func(t *testing.T) {
		uuid := gen.Next()
		if uuid == "" {
			t.Error("Expected non-empty UUID")
		}
		// UUID v4 should be 36 characters (8-4-4-4-12)
		if len(uuid) != 36 {
			t.Errorf("Expected UUID length 36, got %d", len(uuid))
		}
	})

	t.Run("Generate unique UUIDs", func(t *testing.T) {
		uuid1 := gen.Next()
		uuid2 := gen.Next()
		if uuid1 == uuid2 {
			t.Error("Expected different UUIDs")
		}
	})
}

func TestUUID_V7(t *testing.T) {
	gen, err := NewUUID(&UUIDConfig{Version: "v7"})
	if err != nil {
		t.Fatalf("Failed to create UUID v7 generator: %v", err)
	}

	t.Run("Generate v7 UUID", func(t *testing.T) {
		uuid := gen.Next()
		if uuid == "" {
			t.Error("Expected non-empty UUID")
		}
		if len(uuid) != 36 {
			t.Errorf("Expected UUID length 36, got %d", len(uuid))
		}
	})
}

func TestNewSnowflake_Redis(t *testing.T) {
	redis := setupRedisConn(t)
	if redis == nil {
		t.Skip("Redis not available")
	}

	logger := setupLogger(t)

	tests := []struct {
		name        string
		cfg         *SnowflakeConfig
		redis       connector.RedisConnector
		etcd        connector.EtcdConnector
		opts        []Option
		expectError bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			redis:       redis,
			expectError: true,
		},
		{
			name: "valid config with static method",
			cfg: &SnowflakeConfig{
				Method:   "static",
				WorkerID: 1,
			},
			expectError: false,
		},
		{
			name: "valid config with redis method and logger",
			cfg: &SnowflakeConfig{
				Method:       "redis",
				DatacenterID: 1,
				TTL:          30,
			},
			redis:       redis,
			opts:        []Option{WithLogger(logger)},
			expectError: false,
		},
		{
			name: "invalid method",
			cfg: &SnowflakeConfig{
				Method: "invalid",
			},
			expectError: true,
		},
		{
			name: "redis method without connector",
			cfg: &SnowflakeConfig{
				Method: "redis",
			},
			redis:       nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewSnowflake(tt.cfg, tt.redis, tt.etcd, tt.opts...)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if gen == nil {
				t.Error("Expected generator but got nil")
			}
		})
	}
}

func TestSnowflake_NextInt64(t *testing.T) {
	redis := setupRedisConn(t)
	if redis == nil {
		t.Skip("Redis not available")
	}

	gen, err := NewSnowflake(&SnowflakeConfig{
		Method:   "static",
		WorkerID: 1,
	}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create Snowflake generator: %v", err)
	}

	t.Run("Generate Snowflake ID", func(t *testing.T) {
		id, err := gen.NextInt64()
		if err != nil {
			t.Errorf("Failed to generate ID: %v", err)
		}
		if id == 0 {
			t.Error("Expected non-zero ID")
		}
		if id < 0 {
			t.Error("Expected positive ID")
		}
	})

	t.Run("Generate unique IDs", func(t *testing.T) {
		id1, err := gen.NextInt64()
		if err != nil {
			t.Fatalf("Failed to generate first ID: %v", err)
		}

		id2, err := gen.NextInt64()
		if err != nil {
			t.Fatalf("Failed to generate second ID: %v", err)
		}

		if id1 == id2 {
			t.Error("Expected different IDs")
		}
		if id1 >= id2 {
			t.Error("Expected IDs to be in increasing order")
		}
	})

	t.Run("Next method returns string", func(t *testing.T) {
		idStr := gen.Next()
		if idStr == "" {
			t.Error("Expected non-empty string")
		}
		// Should be parseable as int64
		if _, err := strconv.ParseInt(idStr, 10, 64); err != nil {
			t.Errorf("Failed to parse ID as int64: %v", err)
		}
	})
}

func TestNewSequencer(t *testing.T) {
	redis := setupRedisConn(t)
	if redis == nil {
		t.Skip("Redis not available")
	}

	logger := setupLogger(t)

	tests := []struct {
		name        string
		cfg         *SequenceConfig
		redis       connector.RedisConnector
		opts        []Option
		expectError bool
	}{
		{
			name:        "nil config",
			cfg:         nil,
			redis:       redis,
			expectError: true,
		},
		{
			name: "nil redis connector",
			cfg: &SequenceConfig{
				KeyPrefix: "test:",
				Step:      1,
			},
			redis:       nil,
			expectError: true,
		},
		{
			name: "valid config without logger",
			cfg: &SequenceConfig{
				KeyPrefix: "seq:",
				Step:      1,
			},
			redis:       redis,
			expectError: false,
		},
		{
			name: "valid config with logger",
			cfg: &SequenceConfig{
				KeyPrefix: "seq:",
				Step:      1,
				MaxValue:  1000000,
			},
			redis:       redis,
			opts:        []Option{WithLogger(logger)},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen, err := NewSequencer(tt.cfg, tt.redis, tt.opts...)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if gen == nil {
				t.Error("Expected sequencer but got nil")
			}
		})
	}
}

func TestSequencer_Next(t *testing.T) {
	redis := setupRedisConn(t)
	if redis == nil {
		t.Skip("Redis not available")
	}

	logger := setupLogger(t)
	gen, err := NewSequencer(&SequenceConfig{
		KeyPrefix: "test:seq:",
		Step:      1,
	}, redis, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create sequencer: %v", err)
	}

	ctx := context.Background()

	t.Run("Generate sequence numbers", func(t *testing.T) {
		seq1, err := gen.Next(ctx, "user:1")
		if err != nil {
			t.Errorf("Failed to generate sequence: %v", err)
		}
		if seq1 <= 0 {
			t.Error("Expected positive sequence number")
		}

		seq2, err := gen.Next(ctx, "user:1")
		if err != nil {
			t.Errorf("Failed to generate sequence: %v", err)
		}
		if seq2 <= seq1 {
			t.Error("Expected increasing sequence numbers")
		}
	})

	t.Run("Different keys have independent sequences", func(t *testing.T) {
		seq1, err := gen.Next(ctx, "user:100")
		if err != nil {
			t.Fatalf("Failed to generate sequence: %v", err)
		}

		seq2, err := gen.Next(ctx, "user:200")
		if err != nil {
			t.Fatalf("Failed to generate sequence: %v", err)
		}

		// Both should be independent
		seq1Next, err := gen.Next(ctx, "user:100")
		if err != nil {
			t.Fatalf("Failed to generate sequence: %v", err)
		}

		if seq1Next <= seq1 {
			t.Error("Expected user:100 sequence to increment")
		}

		// seq2 should be equal to 1 or 2 based on timing, not affected by user:100
		if seq2 == seq1Next {
			t.Logf("Sequences happen to match: both at %d", seq1Next)
		}
	})
}

func TestSequencer_NextBatch(t *testing.T) {
	redis := setupRedisConn(t)
	if redis == nil {
		t.Skip("Redis not available")
	}

	logger := setupLogger(t)
	gen, err := NewSequencer(&SequenceConfig{
		KeyPrefix: "test:batch:",
		Step:      1,
	}, redis, WithLogger(logger))
	if err != nil {
		t.Fatalf("Failed to create sequencer: %v", err)
	}

	ctx := context.Background()

	t.Run("Batch generate sequences", func(t *testing.T) {
		seqs, err := gen.NextBatch(ctx, "batch:1", 5)
		if err != nil {
			t.Errorf("Failed to generate batch: %v", err)
		}
		if len(seqs) != 5 {
			t.Errorf("Expected 5 sequences, got %d", len(seqs))
		}

		// Check sequences are in order and consecutive
		for i := 0; i < len(seqs)-1; i++ {
			if seqs[i] >= seqs[i+1] {
				t.Errorf("Expected increasing sequences, got %d >= %d", seqs[i], seqs[i+1])
			}
		}
	})

	t.Run("Batch with step", func(t *testing.T) {
		gen2, err := NewSequencer(&SequenceConfig{
			KeyPrefix: "test:step:",
			Step:      5,
		}, redis)
		if err != nil {
			t.Fatalf("Failed to create sequencer with step: %v", err)
		}

		seqs, err := gen2.NextBatch(ctx, "step:1", 3)
		if err != nil {
			t.Errorf("Failed to generate batch: %v", err)
		}
		if len(seqs) != 3 {
			t.Errorf("Expected 3 sequences, got %d", len(seqs))
		}

		// First should be 5, second 10, third 15
		if seqs[0] != 5 {
			t.Errorf("Expected first sequence 5, got %d", seqs[0])
		}
		if seqs[1] != 10 {
			t.Errorf("Expected second sequence 10, got %d", seqs[1])
		}
		if seqs[2] != 15 {
			t.Errorf("Expected third sequence 15, got %d", seqs[2])
		}
	})
}
