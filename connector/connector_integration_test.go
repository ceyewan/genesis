//go:build integration
// +build integration

package connector

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

// getTestLogger 返回测试用日志记录器
func getTestLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("connector-test"))
	if err != nil {
		return clog.Discard()
	}
	return logger
}

// getRedisTestConfig 返回 Redis 测试配置
func getRedisTestConfig() *RedisConfig {
	return &RedisConfig{
		Name:     "test-redis",
		Addr:     getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		DB:       1,
		PoolSize: 10,
	}
}

// getMySQLTestConfig 返回 MySQL 测试配置
func getMySQLTestConfig() *MySQLConfig {
	return &MySQLConfig{
		Name:     "test-mysql",
		Host:     getEnvOrDefault("MYSQL_HOST", "localhost"),
		Port:     getEnvIntOrDefault("MYSQL_PORT", 3306),
		Username: getEnvOrDefault("MYSQL_USER", "genesis_user"),
		Password: getEnvOrDefault("MYSQL_PASSWORD", "genesis_password"),
		Database: getEnvOrDefault("MYSQL_DATABASE", "genesis_db"),
	}
}

// getEtcdTestConfig 返回 Etcd 测试配置
func getEtcdTestConfig() *EtcdConfig {
	return &EtcdConfig{
		Name:        "test-etcd",
		Endpoints:   []string{getEnvOrDefault("ETCD_ENDPOINTS", "localhost:2379")},
		DialTimeout: 5 * time.Second,
	}
}

// getNATSTestConfig 返回 NATS 测试配置
func getNATSTestConfig() *NATSConfig {
	return &NATSConfig{
		Name:          "test-nats",
		URL:           getEnvOrDefault("NATS_URL", "nats://localhost:4222"),
		MaxReconnects: 10,
		ReconnectWait: 100 * time.Millisecond,
	}
}

// getKafkaTestConfig 返回 Kafka 测试配置
func getKafkaTestConfig() *KafkaConfig {
	return &KafkaConfig{
		Name:           "test-kafka",
		Seed:           []string{getEnvOrDefault("KAFKA_BROKERS", "localhost:9092")},
		ConnectTimeout: 10 * time.Second,
		RequestTimeout: 5 * time.Second,
	}
}

// getEnvOrDefault 获取环境变量或返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvIntOrDefault 获取整数环境变量或返回默认值
func getEnvIntOrDefault(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := sscanfInt(value, &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// sscanfInt 简单的整数解析
func sscanfInt(s string, i *int) (int, error) {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	*i = n
	return 1, nil
}

// newTestID 返回唯一的测试 ID
func newTestID() string {
	return time.Now().Format("20060102150405")
}

// TestRedisConnectorIntegration 测试 Redis 连接器完整生命周期
func TestRedisConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("完整生命周期: New -> Connect -> Use -> Close", func(t *testing.T) {
		cfg := getRedisTestConfig()
		conn, err := NewRedis(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)
		require.NotNil(t, conn)

		assert.Equal(t, cfg.Name, conn.Name())
		assert.False(t, conn.IsHealthy())

		ctx := context.Background()

		err = conn.Connect(ctx)
		if err != nil {
			t.Skip("Redis 服务不可用")
		}
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		client := conn.GetClient()
		require.NotNil(t, client)

		testKey := "test:connector:" + newTestID()
		err = client.Set(ctx, testKey, "test-value", time.Minute).Err()
		require.NoError(t, err)

		val, err := client.Get(ctx, testKey).Result()
		require.NoError(t, err)
		assert.Equal(t, "test-value", val)

		client.Del(ctx, testKey)

		err = conn.HealthCheck(ctx)
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		err = conn.Close()
		require.NoError(t, err)
		assert.False(t, conn.IsHealthy())
	})

	t.Run("健康检查失败场景", func(t *testing.T) {
		cfg := &RedisConfig{
			Name: "test-redis-fail",
			Addr: "localhost:9999",
		}
		conn, err := NewRedis(cfg)
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.Error(t, err)
		assert.False(t, conn.IsHealthy())

		conn.Close()
	})

	t.Run("连接器幂等性测试", func(t *testing.T) {
		cfg := getRedisTestConfig()
		conn, err := NewRedis(cfg)
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()

		err = conn.Connect(ctx)
		if err != nil {
			t.Skip("Redis 服务不可用")
		}

		err = conn.Connect(ctx)
		require.NoError(t, err)

		assert.True(t, conn.IsHealthy())
	})
}

// TestMySQLConnectorIntegration 测试 MySQL 连接器
func TestMySQLConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		cfg := getMySQLTestConfig()
		conn, err := NewMySQL(cfg, WithLogger(getTestLogger()))
		if err != nil {
			t.Skip("MySQL 配置无效或服务不可用")
		}

		assert.Equal(t, cfg.Name, conn.Name())
		assert.False(t, conn.IsHealthy())

		ctx := context.Background()

		err = conn.Connect(ctx)
		if err != nil {
			t.Skip("MySQL 服务不可用")
			conn.Close()
		}
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		db := conn.GetClient()
		require.NotNil(t, db)

		var result string
		err = db.Raw("SELECT 1 as val").Scan(&result).Error
		require.NoError(t, err)
		assert.Equal(t, "1", result)

		err = conn.HealthCheck(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
		assert.False(t, conn.IsHealthy())
	})
}

// TestEtcdConnectorIntegration 测试 Etcd 连接器
func TestEtcdConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		cfg := getEtcdTestConfig()
		conn, err := NewEtcd(cfg, WithLogger(getTestLogger()))
		if err != nil {
			t.Skip("Etcd 配置无效")
		}

		ctx := context.Background()

		err = conn.Connect(ctx)
		if err != nil {
			t.Skip("Etcd 服务不可用")
			conn.Close()
		}
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		client := conn.GetClient()
		require.NotNil(t, client)

		testKey := "/test/connector/" + newTestID()

		_, err = client.Put(ctx, testKey, "test-value")
		require.NoError(t, err)

		resp, err := client.Get(ctx, testKey)
		require.NoError(t, err)
		assert.Len(t, resp.Kvs, 1)
		assert.Equal(t, "test-value", string(resp.Kvs[0].Value))

		_, err = client.Delete(ctx, testKey)
		require.NoError(t, err)

		err = conn.HealthCheck(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
	})
}

// TestNATSConnectorIntegration 测试 NATS 连接器
func TestNATSConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		cfg := getNATSTestConfig()
		conn, err := NewNATS(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()

		err = conn.Connect(ctx)
		if err != nil {
			t.Skip("NATS 服务不可用")
		}
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		nc := conn.GetClient()
		require.NotNil(t, nc)
		assert.Equal(t, "nats.CONNECTED", nc.Status().String())

		subject := "test.connector." + newTestID()
		sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
			msg.Respond([]byte("response"))
		})
		require.NoError(t, err)
		defer sub.Unsubscribe()

		err = nc.Publish(subject, []byte("test-message"))
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		err = conn.HealthCheck(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
		assert.False(t, conn.IsHealthy())
	})
}

// TestKafkaConnectorIntegration 测试 Kafka 连接器
func TestKafkaConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		cfg := getKafkaTestConfig()
		conn, err := NewKafka(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()

		err = conn.Connect(ctx)
		if err != nil {
			t.Skip("Kafka 服务不可用")
		}
		assert.True(t, conn.IsHealthy())

		client := conn.GetClient()
		require.NotNil(t, client)

		assert.Equal(t, cfg, conn.Config())

		err = conn.HealthCheck(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
		assert.False(t, conn.IsHealthy())
	})
}

// TestSQLiteConnectorIntegration 测试 SQLite 连接器
func TestSQLiteConnectorIntegration(t *testing.T) {
	t.Run("内存数据库完整生命周期", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		assert.Contains(t, conn.Name(), "sqlite")
		assert.False(t, conn.IsHealthy())

		ctx := context.Background()

		err = conn.Connect(ctx)
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		db := conn.GetClient()
		require.NotNil(t, db)

		err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)").Error
		require.NoError(t, err)

		err = db.Exec("INSERT INTO test (name) VALUES (?)", "test-name").Error
		require.NoError(t, err)

		var name string
		err = db.Raw("SELECT name FROM test WHERE id = 1").Scan(&name).Error
		require.NoError(t, err)
		assert.Equal(t, "test-name", name)

		err = conn.HealthCheck(ctx)
		require.NoError(t, err)

		err = conn.Close()
		require.NoError(t, err)
		assert.False(t, conn.IsHealthy())
	})

	t.Run("文件数据库持久化", func(t *testing.T) {
		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "test.db")

		cfg := &SQLiteConfig{Path: dbPath}
		conn, err := NewSQLite(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()

		err = conn.Connect(ctx)
		require.NoError(t, err)

		db := conn.GetClient()
		err = db.Exec("CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT)").Error
		require.NoError(t, err)

		err = db.Exec("INSERT INTO users (email) VALUES (?)", "test@example.com").Error
		require.NoError(t, err)

		conn.Close()

		conn2, err := NewSQLite(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer conn2.Close()

		err = conn2.Connect(ctx)
		require.NoError(t, err)

		var email string
		err = conn2.GetClient().Raw("SELECT email FROM users WHERE id = 1").Scan(&email).Error
		require.NoError(t, err)
		assert.Equal(t, "test@example.com", email)
	})

	t.Run("并发读写测试", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()

		db := conn.GetClient()

		err = db.Exec("CREATE TABLE counter (id INTEGER PRIMARY KEY, count INTEGER)").Error
		require.NoError(t, err)

		err = db.Exec("INSERT INTO counter (id, count) VALUES (1, 0)").Error
		require.NoError(t, err)

		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				db.Exec("UPDATE counter SET count = count + 1 WHERE id = 1")
			}()
		}
		wg.Wait()

		var count int
		db.Raw("SELECT count FROM counter WHERE id = 1").Scan(&count)
		assert.GreaterOrEqual(t, count, 1)
	})
}

// TestConnectorEnvVarConfig 测试环境变量配置
func TestConnectorEnvVarConfig(t *testing.T) {
	t.Run("MySQL 环境变量配置", func(t *testing.T) {
		os.Setenv("MYSQL_HOST", "localhost")
		os.Setenv("MYSQL_PORT", "3306")
		os.Setenv("MYSQL_USER", "test_user")
		os.Setenv("MYSQL_PASSWORD", "test_pass")
		os.Setenv("MYSQL_DATABASE", "test_db")
		defer func() {
			os.Unsetenv("MYSQL_HOST")
			os.Unsetenv("MYSQL_PORT")
			os.Unsetenv("MYSQL_USER")
			os.Unsetenv("MYSQL_PASSWORD")
			os.Unsetenv("MYSQL_DATABASE")
		}()

		cfg := getMySQLTestConfig()

		assert.Equal(t, "localhost", cfg.Host)
		assert.Equal(t, 3306, cfg.Port)
		assert.Equal(t, "test_user", cfg.Username)
		assert.Equal(t, "test_pass", cfg.Password)
		assert.Equal(t, "test_db", cfg.Database)
	})
}
