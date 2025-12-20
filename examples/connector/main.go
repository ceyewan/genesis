// examples/connector/basic-connection/main.go
package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/metrics"
	"github.com/joho/godotenv"
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

func main() {
	ctx := context.Background()

	// 0. 加载环境变量（从根目录）
	if err := godotenv.Load("/Users/ceyewan/CodeField/genesis/.env"); err != nil {
		log.Printf("Warning: could not load .env file: %v", err)
	}

	// 1. 创建 Logger
	logger, err := clog.New(&clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		EnableColor: true,
		SourceRoot:  "genesis",
	}, &clog.Option{})
	if err != nil {
		log.Fatalf("create logger: %v", err)
	}

	// 2. 创建 Metrics
	meter, err := metrics.New(&metrics.Config{
		Enabled:     true,
		ServiceName: "connector-basic-test",
		Version:     "1.0.0",
		Port:        9092,
		Path:        "/metrics",
	})
	if err != nil {
		log.Fatalf("create metrics: %v", err)
	}
	defer meter.Shutdown(ctx)

	logger.Info("=== Genesis Connector 基本连接测试程序启动 ===")

	// 3. 执行基本连接测试
	runBasicConnectionTests(ctx, logger, meter)

	logger.Info("=== 基本连接测试完成 ===")
	log.Println("Metrics available at: http://localhost:9092/metrics")
}

// runBasicConnectionTests 运行基本连接测试
func runBasicConnectionTests(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== 开始基本连接测试 ===")

	// 测试 Redis 基本连接
	testRedisBasicConnection(ctx, logger, meter)

	// 测试 MySQL 基本连接
	testMySQLBasicConnection(ctx, logger, meter)

	// 测试 Etcd 基本连接
	testEtcdBasicConnection(ctx, logger, meter)

	// 测试 NATS 基本连接
	testNATSBasicConnection(ctx, logger, meter)
}

// testRedisBasicConnection 测试 Redis 基本连接功能
func testRedisBasicConnection(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== Redis 基本连接测试 ===")

	cfg := &connector.RedisConfig{
		BaseConfig: connector.BaseConfig{
			Name: "redis-basic-test",
		},
		Addr:         getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	conn, err := connector.NewRedis(cfg, connector.WithLogger(logger), connector.WithMeter(meter))
	if err != nil {
		logger.Error("创建 Redis 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	// 1. 测试连接
	logger.Info("正在连接 Redis...")
	if err := conn.Connect(ctx); err != nil {
		logger.Error("Redis 连接失败", clog.Error(err))
		return
	}
	logger.Info("Redis 连接成功")

	// 2. 查看连接信息
	logger.Info("=== Redis 连接信息 ===")
	printRedisConnectionInfo(conn, logger)

	// 3. 健康检查
	logger.Info("执行健康检查...")
	if err := conn.HealthCheck(ctx); err != nil {
		logger.Error("Redis 健康检查失败", clog.Error(err))
	} else {
		logger.Info("Redis 健康检查成功")
	}

	// 4. 基本操作测试
	logger.Info("执行基本操作测试...")
	client := conn.GetClient()

	// 测试设置和获取
	testKey := "basic_test_key"
	testValue := "basic_test_value"

	if err := client.Set(ctx, testKey, testValue, time.Minute).Err(); err != nil {
		logger.Error("Redis SET 操作失败", clog.Error(err))
	} else {
		logger.Info("Redis SET 操作成功", clog.String("key", testKey), clog.String("value", testValue))
	}

	if result, err := client.Get(ctx, testKey).Result(); err != nil {
		logger.Error("Redis GET 操作失败", clog.Error(err))
	} else {
		logger.Info("Redis GET 操作成功", clog.String("key", testKey), clog.String("value", result))
	}

	// 5. 多次健康检查
	logger.Info("执行多次健康检查...")
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Second)
		if err := conn.HealthCheck(ctx); err != nil {
			logger.Warn("健康检查失败", clog.Error(err), clog.Int("attempt", i+1))
		} else {
			logger.Info("健康检查成功", clog.Int("attempt", i+1))
		}
	}

	// 6. 断开连接
	logger.Info("正在断开 Redis 连接...")
	conn.Close()
	logger.Info("Redis 连接已断开")
}

// testMySQLBasicConnection 测试 MySQL 基本连接功能
func testMySQLBasicConnection(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== MySQL 基本连接测试 ===")

	cfg := &connector.MySQLConfig{
		BaseConfig: connector.BaseConfig{
			Name: "mysql-basic-test",
		},
		Host:         getEnvOrDefault("MYSQL_HOST", "localhost"),
		Port:         getEnvIntOrDefault("MYSQL_PORT", 3306),
		Username:     getEnvOrDefault("MYSQL_USER", "root"),
		Password:     getEnvOrDefault("MYSQL_PASSWORD", "password"),
		Database:     getEnvOrDefault("MYSQL_DATABASE", "genesis_db"),
		Charset:      "utf8mb4",
		Timeout:      10 * time.Second,
		MaxIdleConns: 5,
		MaxOpenConns: 20,
		MaxLifetime:  time.Hour,
	}

	conn, err := connector.NewMySQL(cfg, connector.WithLogger(logger), connector.WithMeter(meter))
	if err != nil {
		logger.Error("创建 MySQL 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	// 1. 测试连接
	logger.Info("正在连接 MySQL...")
	if err := conn.Connect(ctx); err != nil {
		logger.Error("MySQL 连接失败", clog.Error(err))
		return
	}
	logger.Info("MySQL 连接成功")

	// 2. 查看连接信息
	logger.Info("=== MySQL 连接信息 ===")
	printMySQLConnectionInfo(conn, logger)

	// 3. 健康检查
	logger.Info("执行健康检查...")
	if err := conn.HealthCheck(ctx); err != nil {
		logger.Error("MySQL 健康检查失败", clog.Error(err))
	} else {
		logger.Info("MySQL 健康检查成功")
	}

	// 4. 基本操作测试
	logger.Info("执行基本操作测试...")
	db := conn.GetClient()

	// 测试查询
	var version string
	if err := db.Raw("SELECT VERSION()").Scan(&version).Error; err != nil {
		logger.Error("MySQL 查询失败", clog.Error(err))
	} else {
		logger.Info("MySQL 查询成功", clog.String("version", version))
	}

	// 5. 断开连接
	logger.Info("正在断开 MySQL 连接...")
	conn.Close()
	logger.Info("MySQL 连接已断开")
}

// testEtcdBasicConnection 测试 Etcd 基本连接功能
func testEtcdBasicConnection(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== Etcd 基本连接测试 ===")

	cfg := &connector.EtcdConfig{
		BaseConfig: connector.BaseConfig{
			Name: "etcd-basic-test",
		},
		Endpoints:        []string{"localhost:2379"},
		Username:         "",
		Password:         "",
		DialTimeout:      5 * time.Second,
		Timeout:          5 * time.Second,
		KeepAliveTime:    10 * time.Second,
		KeepAliveTimeout: 3 * time.Second,
	}

	conn, err := connector.NewEtcd(cfg, connector.WithLogger(logger), connector.WithMeter(meter))
	if err != nil {
		logger.Error("创建 Etcd 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	// 1. 测试连接
	logger.Info("正在连接 Etcd...")
	if err := conn.Connect(ctx); err != nil {
		logger.Error("Etcd 连接失败", clog.Error(err))
		return
	}
	logger.Info("Etcd 连接成功")

	// 2. 查看连接信息
	logger.Info("=== Etcd 连接信息 ===")
	printEtcdConnectionInfo(conn, logger)

	// 3. 健康检查
	logger.Info("执行健康检查...")
	if err := conn.HealthCheck(ctx); err != nil {
		logger.Error("Etcd 健康检查失败", clog.Error(err))
	} else {
		logger.Info("Etcd 健康检查成功")
	}

	// 4. 基本操作测试
	logger.Info("执行基本操作测试...")
	client := conn.GetClient()

	testKey := "/basic/test/key"
	testValue := "basic_etcd_value"

	// 设置键值
	if _, err := client.Put(ctx, testKey, testValue); err != nil {
		logger.Error("Etcd PUT 操作失败", clog.Error(err))
	} else {
		logger.Info("Etcd PUT 操作成功", clog.String("key", testKey), clog.String("value", testValue))
	}

	// 获取键值
	if resp, err := client.Get(ctx, testKey); err != nil {
		logger.Error("Etcd GET 操作失败", clog.Error(err))
	} else if len(resp.Kvs) > 0 {
		logger.Info("Etcd GET 操作成功", clog.String("key", testKey), clog.String("value", string(resp.Kvs[0].Value)))
	}

	// 5. 断开连接
	logger.Info("正在断开 Etcd 连接...")
	conn.Close()
	logger.Info("Etcd 连接已断开")
}

// testNATSBasicConnection 测试 NATS 基本连接功能
func testNATSBasicConnection(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== NATS 基本连接测试 ===")

	cfg := &connector.NATSConfig{
		BaseConfig: connector.BaseConfig{
			Name: "nats-basic-test",
		},
		URL:           "nats://localhost:4222",
		Name:          "basic-test-client",
		Username:      "",
		Password:      "",
		Token:         "",
		ReconnectWait: 2 * time.Second,
		MaxReconnects: 60,
		PingInterval:  2 * time.Minute,
		MaxPingsOut:   2,
		Timeout:       5 * time.Second,
	}

	conn, err := connector.NewNATS(cfg, connector.WithLogger(logger), connector.WithMeter(meter))
	if err != nil {
		logger.Error("创建 NATS 连接器失败", clog.Error(err))
		return
	}
	defer conn.Close()

	// 1. 测试连接
	logger.Info("正在连接 NATS...")
	if err := conn.Connect(ctx); err != nil {
		logger.Error("NATS 连接失败", clog.Error(err))
		return
	}
	logger.Info("NATS 连接成功")

	// 2. 查看连接信息
	logger.Info("=== NATS 连接信息 ===")
	printNATSConnectionInfo(conn, logger)

	// 3. 健康检查
	logger.Info("执行健康检查...")
	if err := conn.HealthCheck(ctx); err != nil {
		logger.Error("NATS 健康检查失败", clog.Error(err))
	} else {
		logger.Info("NATS 健康检查成功")
	}

	// 4. 基本操作测试
	logger.Info("执行基本操作测试...")
	client := conn.GetClient()

	testSubject := "basic.test.subject"
	testMessage := "basic test message"

	// 发布消息
	if err := client.Publish(testSubject, []byte(testMessage)); err != nil {
		logger.Error("NATS 发布失败", clog.Error(err))
	} else {
		logger.Info("NATS 发布成功", clog.String("subject", testSubject), clog.String("message", testMessage))
	}

	// 检查连接状态
	status := client.Status()
	logger.Info("NATS 连接状态", clog.String("status", status.String()))

	// 5. 断开连接
	logger.Info("正在断开 NATS 连接...")
	conn.Close()
	logger.Info("NATS 连接已断开")
}

// printRedisConnectionInfo 打印 Redis 连接信息
func printRedisConnectionInfo(conn connector.RedisConnector, logger clog.Logger) {
	client := conn.GetClient()

	// 获取 Redis 信息
	info, err := client.Info(context.Background()).Result()
	if err != nil {
		logger.Warn("获取 Redis 信息失败", clog.Error(err))
		return
	}

	logger.Info("Redis 服务器信息", clog.String("info", info))

	// 获取连接池状态
	poolStats := client.PoolStats()
	logger.Info("Redis 连接池状态",
		clog.Int("hits", int(poolStats.Hits)),
		clog.Int("misses", int(poolStats.Misses)),
		clog.Int("total_conns", int(poolStats.TotalConns)),
		clog.Int("idle_conns", int(poolStats.IdleConns)),
		clog.Int("stale_conns", int(poolStats.StaleConns)),
	)
}

// printMySQLConnectionInfo 打印 MySQL 连接信息
func printMySQLConnectionInfo(conn connector.MySQLConnector, logger clog.Logger) {
	db := conn.GetClient()

	// 获取连接池状态
	sqlDB, err := db.DB()
	if err != nil {
		logger.Warn("获取 MySQL 连接池信息失败", clog.Error(err))
		return
	}

	stats := sqlDB.Stats()
	logger.Info("MySQL 连接池状态",
		clog.Int("open_connections", stats.OpenConnections),
		clog.Int("in_use", stats.InUse),
		clog.Int("idle", stats.Idle),
		clog.Int64("wait_count", stats.WaitCount),
		clog.Duration("wait_duration", stats.WaitDuration),
		clog.Int64("max_idle_closed", stats.MaxIdleClosed),
		clog.Int64("max_lifetime_closed", stats.MaxLifetimeClosed),
	)
}

// printEtcdConnectionInfo 打印 Etcd 连接信息
func printEtcdConnectionInfo(conn connector.EtcdConnector, logger clog.Logger) {
	client := conn.GetClient()

	// 获取 Etcd 状态
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.Status(ctx, client.Endpoints()[0])
	if err != nil {
		logger.Warn("获取 Etcd 状态失败", clog.Error(err))
		return
	}

	logger.Info("Etcd 集群状态",
		clog.String("endpoint", client.Endpoints()[0]),
		clog.String("version", status.Version),
		clog.Int64("db_size", status.DbSize),
		clog.Int64("db_size_in_use", status.DbSizeInUse),
		clog.Bool("leader", !status.IsLearner),
	)
}

// printNATSConnectionInfo 打印 NATS 连接信息
func printNATSConnectionInfo(conn connector.NATSConnector, logger clog.Logger) {
	client := conn.GetClient()

	// 获取连接统计信息
	stats := client.Stats()
	logger.Info("NATS 连接统计",
		clog.Int64("in_msgs", int64(stats.InMsgs)),
		clog.Int64("out_msgs", int64(stats.OutMsgs)),
		clog.Int64("in_bytes", int64(stats.InBytes)),
		clog.Int64("out_bytes", int64(stats.OutBytes)),
		clog.Int64("reconnects", int64(stats.Reconnects)),
		clog.String("status", client.Status().String()),
	)
}
