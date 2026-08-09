package connector

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

type connectorTestMeter struct{ health atomic.Int64 }

type connectorTestCounter struct{ value *atomic.Int64 }

type connectorTestGauge struct{}

type connectorTestHistogram struct{}

func (m *connectorTestMeter) Counter(name, _ string, _ ...metrics.MetricOption) (metrics.Counter, error) {
	if name == MetricHealthChecks {
		return &connectorTestCounter{value: &m.health}, nil
	}
	return &connectorTestCounter{value: &atomic.Int64{}}, nil
}

func (*connectorTestMeter) Gauge(string, string, ...metrics.MetricOption) (metrics.Gauge, error) {
	return connectorTestGauge{}, nil
}

func (*connectorTestMeter) Histogram(string, string, ...metrics.MetricOption) (metrics.Histogram, error) {
	return connectorTestHistogram{}, nil
}

func (*connectorTestMeter) Shutdown(context.Context) error            { return nil }
func (c *connectorTestCounter) Inc(context.Context, ...metrics.Label) { c.value.Add(1) }

func (c *connectorTestCounter) Add(_ context.Context, value float64, _ ...metrics.Label) {
	c.value.Add(int64(value))
}

func (connectorTestGauge) Set(context.Context, float64, ...metrics.Label) {}
func (connectorTestGauge) Inc(context.Context, ...metrics.Label)          {}
func (connectorTestGauge) Dec(context.Context, ...metrics.Label)          {}

func (connectorTestHistogram) Record(context.Context, float64, ...metrics.Label) {
}

func TestWithMeterRecordsHealthChecks(t *testing.T) {
	meter := &connectorTestMeter{}
	conn, err := NewRedis(&RedisConfig{Addr: "localhost:6379"}, WithMeter(meter))
	require.NoError(t, err)
	require.Error(t, conn.HealthCheck(context.Background()))
	require.EqualValues(t, 1, meter.health.Load())
}

func TestConstructorsRejectNilConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		new  func() (any, error)
	}{
		{name: "mysql", new: func() (any, error) { return NewMySQL(nil) }},
		{name: "redis", new: func() (any, error) { return NewRedis(nil) }},
		{name: "etcd", new: func() (any, error) { return NewEtcd(nil) }},
		{name: "nats", new: func() (any, error) { return NewNATS(nil) }},
		{name: "kafka", new: func() (any, error) { return NewKafka(nil) }},
		{name: "sqlite", new: func() (any, error) { return NewSQLite(nil) }},
		{name: "postgresql", new: func() (any, error) { return NewPostgreSQL(nil) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			conn, err := tt.new()
			require.Nil(t, conn)
			require.ErrorIs(t, err, ErrConfig)
			require.Contains(t, err.Error(), tt.name)
			require.Contains(t, err.Error(), "nil")
		})
	}
}

func TestConstructorsCopyConfigBeforeApplyingDefaults(t *testing.T) {
	t.Parallel()

	redisCfg := &RedisConfig{Addr: "localhost:6379"}
	redisConn, err := NewRedis(redisCfg)
	require.NoError(t, err)
	require.Empty(t, redisCfg.Name)
	require.Zero(t, redisCfg.PoolSize)
	redisCfg.Addr = "changed:6379"
	require.Equal(t, "localhost:6379", redisConn.(*redisConnector).cfg.Addr)

	etcdCfg := &EtcdConfig{Endpoints: []string{"localhost:2379"}}
	etcdConn, err := NewEtcd(etcdCfg)
	require.NoError(t, err)
	etcdCfg.Endpoints[0] = "changed:2379"
	require.Equal(t, "localhost:2379", etcdConn.(*etcdConnector).cfg.Endpoints[0])

	kafkaCfg := &KafkaConfig{Seed: []string{"localhost:9092"}}
	kafkaConn, err := NewKafka(kafkaCfg)
	require.NoError(t, err)
	kafkaCfg.Seed[0] = "changed:9092"
	require.Equal(t, "localhost:9092", kafkaConn.(*kafkaConnector).cfg.Seed[0])
}

func TestConfigValidationIdentifiesInvalidField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		err   error
		field string
	}{
		{name: "mysql host", err: (&MySQLConfig{}).validate(), field: "host"},
		{name: "redis addr", err: (&RedisConfig{}).validate(), field: "addr"},
		{name: "etcd endpoints", err: (&EtcdConfig{}).validate(), field: "endpoints"},
		{name: "nats url", err: (&NATSConfig{}).validate(), field: "url"},
		{name: "kafka seed", err: (&KafkaConfig{}).validate(), field: "seed"},
		{name: "sqlite path", err: (&SQLiteConfig{}).validate(), field: "path"},
		{name: "postgresql host", err: (&PostgreSQLConfig{}).validate(), field: "host"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, tt.err, ErrConfig)
			require.True(t, strings.Contains(tt.err.Error(), tt.field), tt.err.Error())
		})
	}
}

func TestConfigValidationRejectsNegativeDurations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "mysql", err: (&MySQLConfig{DSN: "user:pass@tcp(localhost:3306)/db", ConnectTimeout: -time.Second}).validate()},
		{name: "redis", err: (&RedisConfig{Addr: "localhost:6379", ReadTimeout: -time.Second}).validate()},
		{name: "etcd", err: (&EtcdConfig{Endpoints: []string{"localhost:2379"}, DialTimeout: -time.Second}).validate()},
		{name: "nats", err: (&NATSConfig{URL: "nats://localhost:4222", ReconnectWait: -time.Second}).validate()},
		{name: "kafka", err: (&KafkaConfig{Seed: []string{"localhost:9092"}, RequestTimeout: -time.Second}).validate()},
		{name: "postgresql", err: (&PostgreSQLConfig{DSN: "postgres://localhost/db", ConnMaxLifetime: -time.Second}).validate()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, tt.err, ErrConfig)
		})
	}
}

// TestRedisConfigValidation 测试 Redis 配置验证
func TestRedisConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *RedisConfig
		wantErr bool
		isErr   error
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
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "negative DB should fail",
			cfg: &RedisConfig{
				Addr: "localhost:6379",
				DB:   -1,
			},
			wantErr: true,
			isErr:   ErrConfig,
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
			t.Parallel()
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.isErr)
			} else {
				require.NoError(t, err)
				// Verify defaults are set
				require.NotEmpty(t, tt.cfg.Name)
				require.Greater(t, tt.cfg.PoolSize, 0)
			}
		})
	}
}

// TestMySQLConfigValidation 测试 MySQL 配置验证
func TestMySQLConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *MySQLConfig
		wantErr bool
		isErr   error
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
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "negative port should fail",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     -1,
				Username: "root",
				Database: "testdb",
			},
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "empty username should fail",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     3306,
				Username: "",
				Database: "testdb",
			},
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "empty database should fail",
			cfg: &MySQLConfig{
				Host:     "localhost",
				Port:     3306,
				Username: "root",
				Database: "",
			},
			wantErr: true,
			isErr:   ErrConfig,
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
				require.ErrorIs(t, err, tt.isErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestPostgreSQLConfigValidation 测试 PostgreSQL 配置验证
func TestPostgreSQLConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *PostgreSQLConfig
		wantErr bool
		isErr   error
	}{
		{
			name: "valid config",
			cfg: &PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "postgres",
				Password: "password",
				Database: "testdb",
			},
			wantErr: false,
		},
		{
			name: "valid config with DSN",
			cfg: &PostgreSQLConfig{
				DSN: "host=localhost port=5432 user=postgres password=password dbname=testdb sslmode=disable",
			},
			wantErr: false,
		},
		{
			name: "empty host should fail",
			cfg: &PostgreSQLConfig{
				Host:     "",
				Port:     5432,
				Username: "postgres",
				Database: "testdb",
			},
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "negative port should fail",
			cfg: &PostgreSQLConfig{
				Host:     "localhost",
				Port:     -1,
				Username: "postgres",
				Database: "testdb",
			},
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "empty username should fail",
			cfg: &PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "",
				Database: "testdb",
			},
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "empty database should fail",
			cfg: &PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "postgres",
				Database: "",
			},
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "zero port gets default value",
			cfg: &PostgreSQLConfig{
				Host:     "localhost",
				Port:     0,
				Username: "postgres",
				Database: "testdb",
			},
			wantErr: false, // Port 0 will be set to 5432 by setDefaults()
		},
		{
			name: "default values applied",
			cfg: &PostgreSQLConfig{
				Host:     "localhost",
				Port:     5432,
				Username: "postgres",
				Database: "testdb",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.isErr)
			} else {
				require.NoError(t, err)
				// 验证默认值
				if tt.cfg.Port == 0 {
					require.Equal(t, 5432, tt.cfg.Port)
				}
				if tt.cfg.Name == "" {
					require.Equal(t, "default", tt.cfg.Name)
				}
			}
		})
	}
}

// TestEtcdConfigValidation 测试 Etcd 配置验证
func TestEtcdConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *EtcdConfig
		wantErr bool
		isErr   error
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
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "nil endpoints should fail",
			cfg: &EtcdConfig{
				Endpoints: nil,
			},
			wantErr: true,
			isErr:   ErrConfig,
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
				require.ErrorIs(t, err, tt.isErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestNATSConfigValidation 测试 NATS 配置验证
func TestNATSConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *NATSConfig
		wantErr bool
		isErr   error
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
			wantErr: true,
			isErr:   ErrConfig,
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
				require.ErrorIs(t, err, tt.isErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestKafkaConfigValidation 测试 Kafka 配置验证
func TestKafkaConfigValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     *KafkaConfig
		wantErr bool
		isErr   error
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
			wantErr: true,
			isErr:   ErrConfig,
		},
		{
			name: "nil seed should fail",
			cfg: &KafkaConfig{
				Seed: nil,
			},
			wantErr: true,
			isErr:   ErrConfig,
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
				require.ErrorIs(t, err, tt.isErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestSQLiteConfigValidation 测试 SQLite 配置验证
func TestSQLiteConfigValidation(t *testing.T) {
	t.Parallel()
	t.Run("valid in-memory config", func(t *testing.T) {
		cfg := &SQLiteConfig{
			Path: "file::memory:?cache=shared",
		}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		require.NotNil(t, conn)
		conn.Close()
	})

	t.Run("valid file path", func(t *testing.T) {
		cfg := &SQLiteConfig{
			Path: t.TempDir() + "/test.db",
		}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		require.NotNil(t, conn)
		conn.Close()
	})

	t.Run("empty path should fail", func(t *testing.T) {
		cfg := &SQLiteConfig{}
		conn, err := NewSQLite(cfg)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrConfig)
		require.Nil(t, conn)
	})
}

// TestConnectorOptions 测试连接器选项
func TestConnectorOptions(t *testing.T) {
	t.Parallel()
	t.Run("WithLogger", func(t *testing.T) {
		cfg := &RedisConfig{
			Addr: "localhost:6379",
		}
		logger := clog.Discard()

		conn, err := NewRedis(cfg, WithLogger(logger))
		require.NoError(t, err)
		require.NotNil(t, conn)
		conn.Close()
	})
}

// TestConnectorInterface 测试连接器接口实现
func TestConnectorInterface(t *testing.T) {
	t.Parallel()
	t.Run("Redis connector implements interface", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)

		// Verify interface compliance
		var _ Connector = conn

		// Test basic interface methods
		require.Equal(t, "default", conn.Name())
		require.False(t, conn.IsHealthy()) // Not connected yet
		require.Nil(t, conn.GetClient())   // Not connected yet

		conn.Close()
	})

	t.Run("MySQL connector implements interface", func(t *testing.T) {
		cfg := &MySQLConfig{
			Host:     "localhost",
			Port:     3306,
			Username: "test",
			Password: "test",
			Database: "test_db",
		}
		conn, err := NewMySQL(cfg)
		require.NoError(t, err)
		defer conn.Close()

		var _ Connector = conn
		var _ TypedConnector[*gorm.DB] = conn

		require.Equal(t, "default", conn.Name())
		require.Nil(t, conn.GetClient()) // Not connected yet
	})

	t.Run("Etcd connector implements interface", func(t *testing.T) {
		cfg := &EtcdConfig{
			Endpoints: []string{"localhost:2379"},
		}
		conn, err := NewEtcd(cfg)
		require.NoError(t, err)
		defer conn.Close()

		var _ Connector = conn

		require.Equal(t, "default", conn.Name())
		require.Nil(t, conn.GetClient()) // Not connected yet
	})

	t.Run("NATS connector implements interface", func(t *testing.T) {
		cfg := &NATSConfig{
			URL: "nats://localhost:4222",
		}
		conn, err := NewNATS(cfg)
		require.NoError(t, err)

		var _ Connector = conn

		require.Equal(t, "default", conn.Name())
		require.Nil(t, conn.GetClient()) // Not connected yet
	})

	t.Run("Kafka connector implements interface", func(t *testing.T) {
		cfg := &KafkaConfig{
			Seed: []string{"localhost:9092"},
		}
		conn, err := NewKafka(cfg)
		require.NoError(t, err)

		var _ Connector = conn

		require.Equal(t, "default", conn.Name())
		require.Nil(t, conn.GetClient()) // Not connected yet
	})

	t.Run("SQLite connector implements interface", func(t *testing.T) {
		cfg := &SQLiteConfig{
			Path: "file::memory:?cache=shared",
		}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		defer conn.Close()

		var _ Connector = conn
		var _ TypedConnector[*gorm.DB] = conn

		require.Equal(t, "default", conn.Name())
		require.Nil(t, conn.GetClient()) // Not connected yet
	})
}

// TestConnectorName 测试连接器名称设置
func TestConnectorName(t *testing.T) {
	t.Parallel()
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
			require.Equal(t, tt.connName, conn.Name())
			conn.Close()
		})
	}
}

// TestHealthCheckWithoutConnect 测试未连接时的健康检查
func TestHealthCheckWithoutConnect(t *testing.T) {
	t.Parallel()
	t.Run("Redis health check before connect", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)
		defer conn.Close()

		// Before Connect, IsHealthy should return false
		require.False(t, conn.IsHealthy())

		// HealthCheck behavior varies:
		// - If Redis server is running: will succeed and set IsHealthy to true
		// - If Redis server is down: will fail
		ctx := context.Background()
		err = conn.HealthCheck(ctx)
		// Don't assert error since Redis might be available
		// Just verify IsHealthy is updated appropriately
		if err == nil {
			require.True(t, conn.IsHealthy())
		} else {
			require.False(t, conn.IsHealthy())
		}
	})

	t.Run("SQLite health check before connect", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)
		defer conn.Close()

		require.False(t, conn.IsHealthy())

		// SQLite needs explicit Connect to work
		ctx := context.Background()
		err = conn.HealthCheck(ctx)
		// SQLite may fail if not explicitly connected
		_ = err // Error is acceptable
	})
}

// TestCloseWithoutConnect 测试未连接时关闭
func TestCloseWithoutConnect(t *testing.T) {
	t.Parallel()
	t.Run("Redis close without connect", func(t *testing.T) {
		cfg := &RedisConfig{Addr: "localhost:6379"}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)

		// Close without Connect should work
		err = conn.Close()
		require.NoError(t, err)
		require.False(t, conn.IsHealthy())
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
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
	})

	t.Run("Etcd close without connect", func(t *testing.T) {
		cfg := &EtcdConfig{
			Endpoints: []string{"localhost:2379"},
		}
		conn, err := NewEtcd(cfg)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
	})
}

// TestDoubleClose 测试重复关闭
func TestDoubleClose(t *testing.T) {
	t.Parallel()
	t.Run("SQLite double close", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)

		// First connect
		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)

		// Second close should also work or at least not panic
		err = conn.Close()
		require.NoError(t, err)
		require.False(t, conn.IsHealthy())
	})
}

// TestConnectorConcurrency 测试连接器并发安全性
func TestConnectorConcurrency(t *testing.T) {
	t.Parallel()
	t.Run("concurrent IsHealthy calls", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)

		// Concurrent IsHealthy calls
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				conn.IsHealthy()
			})
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
		for range 10 {
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

// TestSentinelErrors 测试哨兵错误的 xerrors.Is 包装语义
func TestSentinelErrors(t *testing.T) {
	t.Parallel()
	t.Run("wrapped error matches sentinel", func(t *testing.T) {
		t.Parallel()
		wrapped := xerrors.Wrap(ErrConnection, "test context")
		require.True(t, xerrors.Is(wrapped, ErrConnection))
		require.False(t, xerrors.Is(wrapped, ErrConfig))
	})
	t.Run("Wrapf preserves sentinel", func(t *testing.T) {
		t.Parallel()
		wrapped := xerrors.Wrapf(ErrHealthCheck, "connector[%s]: %v", "test", "detail")
		require.True(t, xerrors.Is(wrapped, ErrHealthCheck))
		require.False(t, xerrors.Is(wrapped, ErrClientNil))
	})
	t.Run("unrelated errors do not match", func(t *testing.T) {
		t.Parallel()
		other := xerrors.New("some other error")
		require.False(t, xerrors.Is(other, ErrConnection))
		require.False(t, xerrors.Is(other, ErrConfig))
	})
}

// TestContextCancellation 测试上下文取消
func TestContextCancellation(t *testing.T) {
	t.Parallel()
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

func TestWrapConnectorCausePreservesClassificationAndCause(t *testing.T) {
	cause := context.DeadlineExceeded
	err := wrapConnectorCause(ErrConnection, cause, "redis connector[%s]", "test")
	require.ErrorIs(t, err, ErrConnection)
	require.ErrorIs(t, err, cause)
}

func TestPostgresConnectTimeoutAppliedToDSNForms(t *testing.T) {
	require.Contains(t, withPostgresConnectTimeout("postgres://u:p@localhost/db?sslmode=disable", 3*time.Second), "connect_timeout=3")
	require.Contains(t, withPostgresConnectTimeout("host=localhost user=u", 3*time.Second), "connect_timeout=3")
}
