// examples/connector/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
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

	// 0. 加载环境变量
	if err := godotenv.Load(".env"); err != nil {
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
		ServiceName: "connector-test",
		Version:     "1.0.0",
		Port:        9091,
		Path:        "/metrics",
	})
	if err != nil {
		log.Fatalf("create metrics: %v", err)
	}
	defer meter.Shutdown(ctx)

	logger.Info("=== Genesis Connector 测试程序启动 ===")

	// 3. 测试所有连接器
	testConnectors(ctx, logger, meter)

	logger.Info("=== 测试完成 ===")
	log.Println("Metrics available at: http://localhost:9091/metrics")
	log.Println("Grafana dashboard: http://localhost:3000")
}

func testConnectors(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	var wg sync.WaitGroup

	// 测试 Redis
	wg.Add(1)
	go func() {
		defer wg.Done()
		testRedisConnector(ctx, logger, meter)
	}()

	// 测试 MySQL (跳过，如果没有数据库)
	wg.Add(1)
	go func() {
		defer wg.Done()
		testMySQLConnector(ctx, logger, meter)
	}()

	// 测试 Etcd (跳过，如果没有 etcd)
	wg.Add(1)
	go func() {
		defer wg.Done()
		testEtcdConnector(ctx, logger, meter)
	}()

	// 测试 NATS (跳过，如果没有 NATS)
	wg.Add(1)
	go func() {
		defer wg.Done()
		testNATSConnector(ctx, logger, meter)
	}()

	wg.Wait()
}

func testRedisConnector(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== 测试 Redis 连接器 ===")

	// 创建多个 Redis 连接器
	redisConns := make([]connector.RedisConnector, 3)
	for i := 0; i < 3; i++ {
		cfg := &connector.RedisConfig{
			BaseConfig: connector.BaseConfig{
				Name: fmt.Sprintf("redis-test-%d", i),
			},
			Addr:         getEnvOrDefault("REDIS_ADDR", "localhost:6379"),
			Password:     os.Getenv("REDIS_PASSWORD"),
			DB:           i, // 使用不同的 DB
			PoolSize:     getEnvIntOrDefault("REDIS_POOL_SIZE", 10),
			MinIdleConns: getEnvIntOrDefault("REDIS_MIN_IDLE_CONNS", 2),
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		}

		conn, err := connector.NewRedis(cfg, connector.WithLogger(logger), connector.WithMeter(meter))
		if err != nil {
			logger.Error("create redis connector failed", clog.Error(err), clog.Int("index", i))
			continue
		}
		defer conn.Close()

		redisConns[i] = conn

		// 尝试连接
		if err := conn.Connect(ctx); err != nil {
			logger.Warn("connect to redis failed", clog.Error(err), clog.Int("index", i))
		} else {
			logger.Info("redis connected", clog.Int("index", i))
		}
	}

	// 模拟使用和健康检查
	for round := 0; round < 10; round++ {
		time.Sleep(2 * time.Second)
		logger.Info("=== Redis 测试轮次 ===", clog.Int("round", round))

		for i, conn := range redisConns {
			if conn == nil {
				continue
			}

			// 健康检查
			if err := conn.HealthCheck(ctx); err != nil {
				logger.Warn("redis health check failed", clog.Error(err), clog.Int("index", i))
			} else {
				logger.Info("redis health check success", clog.Int("index", i))
			}

			// 使用客户端
			client := conn.GetClient()
			key := fmt.Sprintf("test_key_%d_%d", i, round)
			value := fmt.Sprintf("test_value_%d", round)

			// 设置键值
			err := client.Set(ctx, key, value, time.Minute).Err()
			if err != nil {
				logger.Warn("redis set failed", clog.Error(err), clog.String("key", key))
			}

			// 获取键值
			result, err := client.Get(ctx, key).Result()
			if err != nil {
				logger.Warn("redis get failed", clog.Error(err), clog.String("key", key))
			} else {
				logger.Info("redis get success", clog.String("key", key), clog.String("value", result))
			}
		}

		// 模拟连接断开和重连
		if round == 5 {
			logger.Info("=== 模拟连接断开 ===")
			for i, conn := range redisConns {
				if conn != nil {
					logger.Info("closing redis connection", clog.Int("index", i))
					conn.Close()

					// 等待一段时间后重连
					time.Sleep(1 * time.Second)
					if err := conn.Connect(ctx); err != nil {
						logger.Warn("redis reconnect failed", clog.Error(err), clog.Int("index", i))
					} else {
						logger.Info("redis reconnect success", clog.Int("index", i))
					}
				}
			}
			// 重连后跳过本轮的后续操作，避免使用已关闭的连接
			return
		}
	}

	// 最终清理
	for i, conn := range redisConns {
		if conn != nil {
			conn.Close()
			logger.Info("redis connector closed", clog.Int("index", i))
		}
	}
}

func testMySQLConnector(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== 测试 MySQL 连接器 ===")

	cfg := &connector.MySQLConfig{
		BaseConfig: connector.BaseConfig{
			Name: "mysql-test",
		},
		Host:         getEnvOrDefault("MYSQL_HOST", "localhost"),
		Port:         getEnvIntOrDefault("MYSQL_PORT", 3306),
		Username:     getEnvOrDefault("MYSQL_USER", "root"),
		Password:     getEnvOrDefault("MYSQL_PASSWORD", "password"),
		Database:     getEnvOrDefault("MYSQL_DATABASE", "genesis_db"),
		Charset:      "utf8mb4",
		Timeout:      10 * time.Second,
		MaxIdleConns: getEnvIntOrDefault("MYSQL_MAX_IDLE_CONNS", 5),
		MaxOpenConns: getEnvIntOrDefault("MYSQL_MAX_OPEN_CONNS", 20),
		MaxLifetime:  time.Hour,
	}

	conn, err := connector.NewMySQL(cfg, connector.WithLogger(logger), connector.WithMeter(meter))
	if err != nil {
		logger.Warn("create mysql connector failed", clog.Error(err))
		return
	}
	defer conn.Close()

	// 尝试连接
	if err := conn.Connect(ctx); err != nil {
		logger.Warn("connect to mysql failed", clog.Error(err))
	} else {
		logger.Info("mysql connected")
	}

	// 健康检查
	for i := 0; i < 5; i++ {
		time.Sleep(3 * time.Second)
		if err := conn.HealthCheck(ctx); err != nil {
			logger.Warn("mysql health check failed", clog.Error(err), clog.Int("check", i))
		} else {
			logger.Info("mysql health check success", clog.Int("check", i))
		}
	}
}

func testEtcdConnector(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== 测试 Etcd 连接器 ===")

	cfg := &connector.EtcdConfig{
		BaseConfig: connector.BaseConfig{
			Name: "etcd-test",
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
		logger.Warn("create etcd connector failed", clog.Error(err))
		return
	}
	defer conn.Close()

	// 尝试连接
	if err := conn.Connect(ctx); err != nil {
		logger.Warn("connect to etcd failed", clog.Error(err))
	} else {
		logger.Info("etcd connected")
	}

	// 健康检查和基本操作
	client := conn.GetClient()
	for i := 0; i < 5; i++ {
		time.Sleep(3 * time.Second)

		// 健康检查
		if err := conn.HealthCheck(ctx); err != nil {
			logger.Warn("etcd health check failed", clog.Error(err), clog.Int("check", i))
		} else {
			logger.Info("etcd health check success", clog.Int("check", i))
		}

		// 基本操作
		key := fmt.Sprintf("/test/key%d", i)
		value := fmt.Sprintf("value%d", i)

		// 设置键值
		_, err = client.Put(ctx, key, value)
		if err != nil {
			logger.Warn("etcd put failed", clog.Error(err), clog.String("key", key))
		} else {
			logger.Info("etcd put success", clog.String("key", key), clog.String("value", value))
		}

		// 获取键值
		resp, err := client.Get(ctx, key)
		if err != nil {
			logger.Warn("etcd get failed", clog.Error(err), clog.String("key", key))
		} else if len(resp.Kvs) > 0 {
			logger.Info("etcd get success", clog.String("key", key), clog.String("value", string(resp.Kvs[0].Value)))
		}
	}
}

func testNATSConnector(ctx context.Context, logger clog.Logger, meter metrics.Meter) {
	logger.Info("=== 测试 NATS 连接器 ===")

	cfg := &connector.NATSConfig{
		BaseConfig: connector.BaseConfig{
			Name: "nats-test",
		},
		URL:           "nats://localhost:4222",
		Name:          "test-client",
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
		logger.Warn("create nats connector failed", clog.Error(err))
		return
	}
	defer conn.Close()

	// 尝试连接
	if err := conn.Connect(ctx); err != nil {
		logger.Warn("connect to nats failed", clog.Error(err))
	} else {
		logger.Info("nats connected")
	}

	// 健康检查和基本操作
	client := conn.GetClient()
	for i := 0; i < 5; i++ {
		time.Sleep(3 * time.Second)

		// 健康检查
		if err := conn.HealthCheck(ctx); err != nil {
			logger.Warn("nats health check failed", clog.Error(err), clog.Int("check", i))
		} else {
			logger.Info("nats health check success", clog.Int("check", i))
		}

		// 发布消息
		subject := fmt.Sprintf("test.subject.%d", i)
		message := fmt.Sprintf("test message %d", i)

		err = client.Publish(subject, []byte(message))
		if err != nil {
			logger.Warn("nats publish failed", clog.Error(err), clog.String("subject", subject))
		} else {
			logger.Info("nats publish success", clog.String("subject", subject), clog.String("message", message))
		}

		// 检查连接状态
		status := client.Status()
		logger.Info("nats status", clog.String("status", status.String()))
	}
}
