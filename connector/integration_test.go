// 运行测试需要: go test ./connector/... -tags=integration -v
package connector

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcetcd "github.com/testcontainers/testcontainers-go/modules/etcd"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/modules/nats"
	"github.com/testcontainers/testcontainers-go/modules/redis"
	clientv3 "go.etcd.io/etcd/client/v3"
	"gorm.io/gorm"
)

// getTestLogger 返回测试用日志记录器
func getTestLogger() clog.Logger {
	logger, err := clog.New(clog.NewDevDefaultConfig("connector-test"))
	if err != nil {
		return clog.Discard()
	}
	return logger
}

// newTestID 返回唯一的测试 ID
func newTestID() string {
	return time.Now().Format("20060102150405")
}

// =============================================================================
// Redis 集成测试
// =============================================================================

func setupRedisContainer(t *testing.T) (*redis.RedisContainer, *RedisConfig) {
	ctx := context.Background()

	container, err := redis.RunContainer(ctx,
		testcontainers.WithImage("redis:7-alpine"),
	)
	require.NoError(t, err, "Failed to start Redis container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)

	cfg := &RedisConfig{
		Name:     "test-redis",
		Addr:     fmt.Sprintf("%s:%s", host, mappedPort.Port()),
		DB:       0,
		PoolSize: 10,
	}

	return container, cfg
}

func TestRedisConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("完整生命周期: New -> Connect -> Use -> Close", func(t *testing.T) {
		container, cfg := setupRedisContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewRedis(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)
		require.NotNil(t, conn)

		assert.Equal(t, cfg.Name, conn.Name())
		assert.False(t, conn.IsHealthy())

		ctx := context.Background()

		err = conn.Connect(ctx)
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
		container, cfg := setupRedisContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewRedis(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)
		defer conn.Close()

		ctx := context.Background()

		err = conn.Connect(ctx)
		require.NoError(t, err)

		err = conn.Connect(ctx)
		require.NoError(t, err)

		assert.True(t, conn.IsHealthy())
	})
}

// =============================================================================
// MySQL 集成测试
// =============================================================================

func setupMySQLContainer(t *testing.T) (*mysql.MySQLContainer, *MySQLConfig) {
	ctx := context.Background()

	container, err := mysql.RunContainer(ctx,
		mysql.WithDatabase("genesis_db"),
		mysql.WithUsername("genesis_user"),
		mysql.WithPassword("genesis_password"),
	)
	require.NoError(t, err, "Failed to start MySQL container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "3306")
	require.NoError(t, err)

	port, err := strconv.Atoi(mappedPort.Port())
	require.NoError(t, err)

	cfg := &MySQLConfig{
		Name:     "test-mysql",
		Host:     host,
		Port:     port,
		Username: "genesis_user",
		Password: "genesis_password",
		Database: "genesis_db",
	}

	return container, cfg
}

func TestMySQLConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		container, cfg := setupMySQLContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewMySQL(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		assert.Equal(t, cfg.Name, conn.Name())
		assert.False(t, conn.IsHealthy())

		ctx := context.Background()

		err = conn.Connect(ctx)
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

	t.Run("GORM 基本操作", func(t *testing.T) {
		container, cfg := setupMySQLContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewMySQL(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()

		db := conn.GetClient()

		// 创建测试表
		err = db.Exec("CREATE TABLE test_users (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(255))").Error
		require.NoError(t, err)

		// 插入数据
		err = db.Exec("INSERT INTO test_users (name) VALUES (?)", "test-name").Error
		require.NoError(t, err)

		// 查询数据
		var name string
		err = db.Raw("SELECT name FROM test_users WHERE id = 1").Scan(&name).Error
		require.NoError(t, err)
		assert.Equal(t, "test-name", name)
	})
}

// =============================================================================
// Etcd 集成测试
// =============================================================================

func setupEtcdContainer(t *testing.T) (*tcetcd.EtcdContainer, *EtcdConfig) {
	ctx := context.Background()

	container, err := tcetcd.Run(ctx, "quay.io/coreos/etcd:v3.5.9")
	require.NoError(t, err, "Failed to start Etcd container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "2379")
	require.NoError(t, err)

	cfg := &EtcdConfig{
		Name:        "test-etcd",
		Endpoints:   []string{fmt.Sprintf("%s:%s", host, mappedPort.Port())},
		DialTimeout: 5 * time.Second,
	}

	return container, cfg
}

func TestEtcdConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		container, cfg := setupEtcdContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewEtcd(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()

		err = conn.Connect(ctx)
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
		assert.False(t, conn.IsHealthy())
	})

	t.Run("Etcd 客户端基本操作", func(t *testing.T) {
		container, cfg := setupEtcdContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewEtcd(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()

		client := conn.GetClient()

		// 测试事务
		testKeyPrefix := "/test/txn/" + newTestID()

		kv := client.Txn(ctx)
		kv.Then(
			clientv3.OpPut(testKeyPrefix+"/key1", "value1"),
			clientv3.OpPut(testKeyPrefix+"/key2", "value2"),
		)
		_, err = kv.Commit()
		require.NoError(t, err)

		// 获取范围查询
		resp, err := client.Get(ctx, testKeyPrefix+"/", clientv3.WithPrefix())
		require.NoError(t, err)
		assert.Len(t, resp.Kvs, 2)
	})
}

// =============================================================================
// NATS 集成测试
// =============================================================================

func setupNATSContainer(t *testing.T) (*nats.NATSContainer, *NATSConfig) {
	ctx := context.Background()

	container, err := nats.RunContainer(ctx,
		testcontainers.WithImage("nats:2.10-alpine"),
	)
	require.NoError(t, err, "Failed to start NATS container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "4222")
	require.NoError(t, err)

	cfg := &NATSConfig{
		Name:          "test-nats",
		URL:           fmt.Sprintf("nats://%s:%s", host, mappedPort.Port()),
		MaxReconnects: 10,
		ReconnectWait: 100 * time.Millisecond,
	}

	return container, cfg
}

func TestNATSConnectorIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	t.Run("完整生命周期", func(t *testing.T) {
		container, cfg := setupNATSContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewNATS(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()

		err = conn.Connect(ctx)
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())

		nc := conn.GetClient()
		require.NotNil(t, nc)
		assert.Equal(t, natsgo.CONNECTED, nc.Status())

		subject := "test.connector." + newTestID()
		sub, err := nc.Subscribe(subject, func(msg *natsgo.Msg) {
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

	t.Run("NATS 发布订阅", func(t *testing.T) {
		container, cfg := setupNATSContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewNATS(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()

		nc := conn.GetClient()

		subject := "test.pubsub." + newTestID()
		received := make(chan string, 1)

		sub, err := nc.Subscribe(subject, func(msg *natsgo.Msg) {
			received <- string(msg.Data)
		})
		require.NoError(t, err)
		defer sub.Unsubscribe()

		err = nc.Publish(subject, []byte("hello-nats"))
		require.NoError(t, err)

		select {
		case msg := <-received:
			assert.Equal(t, "hello-nats", msg)
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for message")
		}
	})

	t.Run("NATS 连接状态检查", func(t *testing.T) {
		container, cfg := setupNATSContainer(t)
		defer container.Terminate(context.Background())

		conn, err := NewNATS(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()

		nc := conn.GetClient()
		assert.Equal(t, natsgo.CONNECTED, nc.Status())

		// 健康检查应该通过
		err = conn.HealthCheck(ctx)
		require.NoError(t, err)
		assert.True(t, conn.IsHealthy())
	})
}

// =============================================================================
// SQLite 本地测试（无需容器）
// =============================================================================

func TestSQLiteConnectorIntegration(t *testing.T) {
	t.Run("内存数据库完整生命周期", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		assert.Equal(t, "default", conn.Name())
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

	t.Run("GORM CRUD 操作", func(t *testing.T) {
		cfg := &SQLiteConfig{Path: "file::memory:?cache=shared"}
		conn, err := NewSQLite(cfg, WithLogger(getTestLogger()))
		require.NoError(t, err)

		ctx := context.Background()
		err = conn.Connect(ctx)
		require.NoError(t, err)
		defer conn.Close()

		db := conn.GetClient()

		// 自动迁移
		type Product struct {
			ID    uint
			Name  string
			Price float64
		}
		err = db.AutoMigrate(&Product{})
		require.NoError(t, err)

		// 创建记录
		product := Product{Name: "Test Product", Price: 99.99}
		err = db.Create(&product).Error
		require.NoError(t, err)
		assert.Greater(t, product.ID, uint(0))

		// 查询记录
		var result Product
		err = db.First(&result, product.ID).Error
		require.NoError(t, err)
		assert.Equal(t, "Test Product", result.Name)
		assert.Equal(t, 99.99, result.Price)

		// 更新记录
		err = db.Model(&result).Update("Price", 149.99).Error
		require.NoError(t, err)

		// 验证更新
		var updated Product
		db.First(&updated, product.ID)
		assert.Equal(t, 149.99, updated.Price)

		// 删除记录
		err = db.Delete(&result).Error
		require.NoError(t, err)

		// 验证删除
		var deleted Product
		err = db.First(&deleted, product.ID).Error
		assert.Error(t, err)
		assert.True(t, gorm.ErrRecordNotFound == err || strings.Contains(err.Error(), "record not found"))
	})
}
