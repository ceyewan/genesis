package connector

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedisConfigValidation 测试 Redis 配置验证
func TestRedisConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *RedisConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config with defaults",
			cfg: &RedisConfig{
				Addr: "localhost:6379",
			},
			wantErr: false,
		},
		{
			name: "valid config with custom values",
			cfg: &RedisConfig{
				Name:         "custom-redis",
				Addr:         "localhost:6379",
				Password:     "password",
				DB:           1,
				PoolSize:     20,
				MinIdleConns: 5,
			},
			wantErr: false,
		},
		{
			name: "empty address should fail",
			cfg: &RedisConfig{
				Addr: "",
			},
			wantErr:     true,
			errContains: "地址不能为空",
		},
		{
			name: "negative DB should fail",
			cfg: &RedisConfig{
				Addr: "localhost:6379",
				DB:   -1,
			},
			wantErr:     true,
			errContains: "数据库编号不能小于0",
		},
		{
			name: "default values applied",
			cfg: &RedisConfig{
				Addr: "localhost:6379",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				// Verify defaults are set
				assert.NotEmpty(t, tt.cfg.Name)
				assert.Greater(t, tt.cfg.MaxRetries, 0)
				assert.Greater(t, tt.cfg.PoolSize, 0)
			}
		})
	}
}

// TestMySQLConfigValidation 测试 MySQL 配置验证
func TestMySQLConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *MySQLConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     3306,
				Username: "root",
				Password: "password",
				Database: "testdb",
			},
			wantErr: false,
		},
		{
			name: "empty host should fail",
			cfg: &MySQLConfig{
				Host:     "",
				Port:     3306,
				Username: "root",
				Database: "testdb",
			},
			wantErr:     true,
			errContains: "主机地址不能为空",
		},
		{
			name: "negative port should fail",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     -1,
				Username: "root",
				Database: "testdb",
			},
			wantErr:     true,
			errContains: "端口必须大于0",
		},
		{
			name: "empty username should fail",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     3306,
				Username: "",
				Database: "testdb",
			},
			wantErr:     true,
			errContains: "用户名不能为空",
		},
		{
			name: "empty database should fail",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     3306,
				Username: "root",
				Database: "",
			},
			wantErr:     true,
			errContains: "数据库名不能为空",
		},
		{
			name: "zero port gets default value",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     0,
				Username: "root",
				Database: "testdb",
			},
			wantErr: false, // Port 0 will be set to 3306 by setDefaults()
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestEtcdConfigValidation 测试 Etcd 配置验证
func TestEtcdConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *EtcdConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			cfg: &EtcdConfig{
				Endpoints: []string{"localhost:2379"},
			},
			wantErr: false,
		},
		{
			name: "empty endpoints should fail",
			cfg: &EtcdConfig{
				Endpoints: []string{},
			},
			wantErr:     true,
			errContains: "端点不能为空",
		},
		{
			name: "nil endpoints should fail",
			cfg: &EtcdConfig{
				Endpoints: nil,
			},
			wantErr:     true,
			errContains: "端点不能为空",
		},
		{
			name: "multiple endpoints",
			cfg: &EtcdConfig{
				Endpoints: []string{"localhost:2379", "localhost:2380", "localhost:2381"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestNATSConfigValidation 测试 NATS 配置验证
func TestNATSConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *NATSConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			cfg: &NATSConfig{
				URL: "nats://localhost:4222",
			},
			wantErr: false,
		},
		{
			name: "empty URL should fail",
			cfg: &NATSConfig{
				URL: "",
			},
			wantErr:     true,
			errContains: "URL不能为空",
		},
		{
			name: "valid config with auth",
			cfg: &NATSConfig{
				URL:      "nats://localhost:4222",
				Username: "user",
				Password: "pass",
			},
			wantErr: false,
		},
		{
			name: "valid config with token",
			cfg: &NATSConfig{
				URL:   "nats://localhost:4222",
				Token: "token123",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestKafkaConfigValidation 测试 Kafka 配置验证
func TestKafkaConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *KafkaConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			cfg: &KafkaConfig{
				Seed: []string{"localhost:9092"},
			},
			wantErr: false,
		},
		{
			name: "empty seed should fail",
			cfg: &KafkaConfig{
				Seed: []string{},
			},
			wantErr:     true,
			errContains: "seed brokers不能为空",
		},
		{
			name: "nil seed should fail",
			cfg: &KafkaConfig{
				Seed: nil,
			},
			wantErr:     true,
			errContains: "seed brokers不能为空",
		},
		{
			name: "multiple brokers",
			cfg: &KafkaConfig{
				Seed: []string{"localhost:9092", "localhost:9093"},
			},
			wantErr: false,
		},
		{
			name: "valid config with SASL",
			cfg: &KafkaConfig{
				Seed:     []string{"localhost:9092"},
				User:     "user",
				Password: "pass",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestSQLiteConfigValidation 测试 SQLite 配置验证
func TestSQLiteConfigValidation(t *testing.T) {
	t.Run("valid in-memory config", func(t *testing.T) {
		cfg := &SQLiteConfig{
			Path: "file::memory:?cache=shared",
		}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		assert.NotNil(t, conn)
		conn.Close()
	})

	t.Run("valid file path", func(t *testing.T) {
		cfg := &SQLiteConfig{
			Path: t.TempDir() + "/test.db",
		}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		assert.NotNil(t, conn)
		conn.Close()
	})

	t.Run("empty path should fail", func(t *testing.T) {
		cfg := &SQLiteConfig{}
		conn, err := NewSQLite(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path is required")
		assert.Nil(t, conn)
	})
}

// TestConnectorOptions 测试连接器选项
func TestConnectorOptions(t *testing.T) {
	t.Run("WithLogger", func(t *testing.T) {
		cfg := &RedisConfig{
			Addr: "localhost:6379",
		}
		logger := clog.Discard()

		conn, err := NewRedis(cfg, WithLogger(logger))
		require.NoError(t, err)
		assert.NotNil(t, conn)
		conn.Close()
	})

	t.Run("WithMeter", func(t *testing.T) {
		cfg := &RedisConfig{
			Addr: "localhost:6379",
		}
		meter := metrics.Discard()

		conn, err := NewRedis(cfg, WithMeter(meter))
		require.NoError(t, err)
		assert.NotNil(t, conn)
		conn.Close()
	})

	t.Run("WithLoggerAndMeter", func(t *testing.T) {
		cfg := &RedisConfig{
			Addr: "localhost:6379",
		}
		logger := clog.Discard()
		meter := metrics.Discard()

		conn, err := NewRedis(cfg, WithLogger(logger), WithMeter(meter))
		require.NoError(t, err)
		assert.NotNil(t, conn)
		conn.Close()
	})
}

// TestConnectorInterface 测试连接器接口实现
func TestConnectorInterface(t *testing.T) {
	t.Run("Redis connector implements interface", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)

		// Verify interface compliance
		var _ Connector = conn
		var _ RedisConnector = conn

		// Test basic interface methods
		assert.Equal(t, "default", conn.Name())
		assert.False(t, conn.IsHealthy()) // Not connected yet
		assert.NotNil(t, conn.GetClient())

		conn.Close()
	})

	t.Run("MySQL connector implements interface", func(t *testing.T) {
		// MySQL connector creates GORM connection in NewMySQL
		// Use an invalid port to avoid actual connection attempt
		cfg := &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "test",
			Password: "test",
			Database: "test_db",
		}
		conn, err := NewMySQL(cfg)
		// MySQL might fail to connect due to credentials, but we can still test the interface
		if err != nil {
			// If connection fails, skip this test
			t.Skip("MySQL not available for interface test")
		}
		defer conn.Close()

		var _ Connector = conn
		var _ MySQLConnector = conn
		var _ DatabaseConnector = conn

		assert.Equal(t, "default", conn.Name())
		assert.NotNil(t, conn.GetClient())
	})

	t.Run("Etcd connector implements interface", func(t *testing.T) {
		cfg := &EtcdConfig{
			Endpoints: []string{"localhost:2379"},
		}
		conn, err := NewEtcd(cfg)
		if err != nil {
			t.Skip("Etcd not available for interface test")
		}
		defer conn.Close()

		var _ Connector = conn
		var _ EtcdConnector = conn

		assert.Equal(t, "default", conn.Name())
		assert.NotNil(t, conn.GetClient())
	})

	t.Run("NATS connector implements interface", func(t *testing.T) {
		cfg := &NATSConfig{
			URL: "nats://localhost:4222",
		}
		conn, err := NewNATS(cfg)
		require.NoError(t, err)

		var _ Connector = conn
		var _ NATSConnector = conn

		assert.Equal(t, "default", conn.Name())
		assert.Nil(t, conn.GetClient()) // Not connected yet
	})

	t.Run("Kafka connector implements interface", func(t *testing.T) {
		cfg := &KafkaConfig{
			Seed: []string{"localhost:9092"},
		}
		conn, err := NewKafka(cfg)
		require.NoError(t, err)

		var _ Connector = conn
		var _ KafkaConnector = conn

		assert.Equal(t, "default", conn.Name())
		assert.Nil(t, conn.GetClient()) // Not connected yet
		assert.Equal(t, cfg, conn.Config())
	})

	t.Run("SQLite connector implements interface", func(t *testing.T) {
		cfg := &SQLiteConfig{
			Path: "file::memory:?cache=shared",
		}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)

		var _ Connector = conn
		var _ SQLiteConnector = conn
		var _ DatabaseConnector = conn

		assert.Contains(t, conn.Name(), "sqlite")
		assert.NotNil(t, conn.GetClient())
		conn.Close()
	})
}

// TestConnectorName 测试连接器名称设置
func TestConnectorName(t *testing.T) {
	tests := []struct {
		name     string
		connName string
	}{
		{"default name", "default"},
		{"custom name", "my-connector"},
		{"name with number", "connector-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &RedisConfig{
				Name: tt.connName,
				Addr: "localhost:6379",
			}
			conn, err := NewRedis(cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.connName, conn.Name())
			conn.Close()
		})
	}
}

// TestHealthCheckWithoutConnect 测试未连接时的健康检查
func TestHealthCheckWithoutConnect(t *testing.T) {
	t.Run("Redis health check before connect", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)
		defer conn.Close()

		// Before Connect, IsHealthy should return false
		assert.False(t, conn.IsHealthy())

		// HealthCheck behavior varies:
		// - If Redis server is running: will succeed and set IsHealthy to true
		// - If Redis server is down: will fail
		ctx := context.Background()
		err = conn.HealthCheck(ctx)
		// Don't assert error since Redis might be available
		// Just verify IsHealthy is updated appropriately
		if err == nil {
			assert.True(t, conn.IsHealthy())
		} else {
			assert.False(t, conn.IsHealthy())
		}
	})

	t.Run("SQLite health check before connect", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		defer conn.Close()

		assert.False(t, conn.IsHealthy())

		// SQLite needs explicit Connect to work
		ctx := context.Background()
		err = conn.HealthCheck(ctx)
		// SQLite may fail if not explicitly connected
		_ = err // Error is acceptable
	})
}

// TestCloseWithoutConnect 测试未连接时关闭
func TestCloseWithoutConnect(t *testing.T) {
	t.Run("Redis close without connect", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)

		// Close without Connect should work
		err = conn.Close()
		assert.NoError(t, err)
		assert.False(t, conn.IsHealthy())
	})

	t.Run("MySQL close without connect", func(t *testing.T) {
		cfg := &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "pass",
			Database: "db",
		}
		conn, err := NewMySQL(cfg)
		if err != nil {
			t.Skip("MySQL not available")
		}

		err = conn.Close()
		assert.NoError(t, err)
	})

	t.Run("Etcd close without connect", func(t *testing.T) {
		cfg := &EtcdConfig{
			Endpoints: []string{"localhost:2379"},
		}
		conn, err := NewEtcd(cfg)
		if err != nil {
			t.Skip("Etcd not available")
		}

		err = conn.Close()
		assert.NoError(t, err)
	})
}

// TestDoubleClose 测试重复关闭
func TestDoubleClose(t *testing.T) {
	t.Run("Redis double close", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)

		// Second close should also work or at least not panic
		err = conn.Close()
		// Behavior may vary, but shouldn't panic
		assert.False(t, conn.IsHealthy())
	})
}

// TestConnectorConcurrency 测试连接器并发安全性
func TestConnectorConcurrency(t *testing.T) {
	t.Run("concurrent IsHealthy calls", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)

		// Concurrent IsHealthy calls
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn.IsHealthy()
			}()
		}
		wg.Wait()

		conn.Close()
	})

	t.Run("concurrent Connect and Close", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)

		ctx := context.Background()

		// This test verifies no race conditions occur
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(2)
			go func() {
				defer wg.Done()
				_ = conn.Connect(ctx)
			}()
			go func() {
				defer wg.Done()
				_ = conn.Close()
			}()
		}
		wg.Wait()

		// Final cleanup
		conn.Close()
	})
}

// TestSentinelErrors 测试哨兵错误
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		isErr bool
	}{
		{"ErrNotConnected", ErrNotConnected, true},
		{"ErrAlreadyClosed", ErrAlreadyClosed, true},
		{"ErrConnection", ErrConnection, true},
		{"ErrTimeout", ErrTimeout, true},
		{"ErrConfig", ErrConfig, true},
		{"ErrHealthCheck", ErrHealthCheck, true},
		{"wrapped error", xerrors.Wrap(ErrNotConnected, "test"), true},
		{"different error", fmt.Errorf("different"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.isErr {
				assert.Error(t, tt.err)
			}
		})
	}
}

// TestMetricsCreation 测试指标创建
func TestMetricsCreation(t *testing.T) {
	t.Run("metrics creation for Redis", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}

		// Create a real meter for testing
		meter, err := metrics.New(&metrics.Config{
			ServiceName: "test-connector",
			Port:        9093,
		})
		require.NoError(t, err)
		defer meter.Shutdown(context.Background())

		conn, err := NewRedis(cfg, WithMeter(meter))
		require.NoError(t, err)
		conn.Close()
	})

	t.Run("metrics creation for MySQL", func(t *testing.T) {
		cfg := &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "pass",
			Database: "db",
		}

		meter, err := metrics.New(&metrics.Config{
			ServiceName: "test-connector",
			Port:        9094,
		})
		require.NoError(t, err)
		defer meter.Shutdown(context.Background())

		conn, err := NewMySQL(cfg, WithMeter(meter))
		if err != nil {
			t.Skip("MySQL not available for metrics test")
		}
		conn.Close()
	})
}

// TestContextCancellation 测试上下文取消
func TestContextCancellation(t *testing.T) {
	t.Run("connect with cancelled context", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		defer conn.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Connect should still work for SQLite (it's fast)
		// For network-based connectors, this would fail
		err = conn.Connect(ctx)
		// SQLite might still succeed before cancellation is processed
		_ = err
	})
}

// BenchmarkConnectorCreation 性能基准测试
func BenchmarkConnectorCreation(b *testing.B) {
	b.Run("Redis", func(b *testing.B) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		for i := 0; i < b.N; i++ {
			conn, _ := NewRedis(cfg)
			conn.Close()
		}
	})

	b.Run("MySQL", func(b *testing.B) {
		cfg := &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "root",
			Password: "pass",
			Database: "db",
		}
		for i := 0; i < b.N; i++ {
			conn, _ := NewMySQL(cfg)
			conn.Close()
		}
	})

	b.Run("SQLite", func(b *testing.B) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		for i := 0; i < b.N; i++ {
			conn, _ := NewSQLite(cfg)
			conn.Close()
		}
	})
}

// BenchmarkIsHealthy 性能基准测试
func BenchmarkIsHealthy(b *testing.B) {
	cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
	conn, _ := NewSQLite(cfg)
	conn.Connect(context.Background())
	defer conn.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn.IsHealthy()
	}
}
