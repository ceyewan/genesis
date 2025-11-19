// examples/connector/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/container"
)

func main() {
	// 创建容器配置
	cfg := &container.Config{
		MySQL: &connector.MySQLConfig{
			Host:         "127.0.0.1",
			Port:         3306,
			Username:     "root",
			Password:     "your_root_password",
			Database:     "app_db",
			Charset:      "utf8mb4",
			Timeout:      10 * time.Second,
			MaxIdleConns: 10,
			MaxOpenConns: 100,
			MaxLifetime:  time.Hour,
		},
		Redis: &connector.RedisConfig{
			Addr:         "127.0.0.1:6379",
			Password:     "your_redis_password",
			DB:           0,
			PoolSize:     10,
			MinIdleConns: 5,
			MaxRetries:   3,
			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
		},
		Etcd: &connector.EtcdConfig{
			Endpoints:        []string{"127.0.0.1:2379"},
			Username:         "",
			Password:         "",
			Timeout:          5 * time.Second,
			KeepAliveTime:    10 * time.Second,
			KeepAliveTimeout: 3 * time.Second,
		},
		NATS: &connector.NATSConfig{
			URL:           "nats://127.0.0.1:4222",
			Name:          "demo-client",
			Username:      "",
			Password:      "",
			ReconnectWait: 2 * time.Second,
			MaxReconnects: 60,
			PingInterval:  2 * time.Minute,
			MaxPingsOut:   2,
			Timeout:       5 * time.Second,
		},
	}

	// 创建并启动容器
	app, err := container.New(cfg)
	if err != nil {
		panic(fmt.Sprintf("创建容器失败: %v", err))
	}
	defer app.Close()

	// 使用日志
	fmt.Println("应用启动成功")

	// 测试 MySQL 连接
	testMySQL(app)

	// 测试 Redis 连接
	testRedis(app)

	// 测试 Etcd 连接
	testEtcd(app)

	// 测试 NATS 连接
	testNATS(app)

	// 等待中断信号
	waitForShutdown(app)
}

func testMySQL(app *container.Container) {
	// 获取 MySQL 连接器
	mysqlConfig := connector.MySQLConfig{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "your_root_password",
		Database:     "app_db",
		Charset:      "utf8mb4",
		Timeout:      10 * time.Second,
		MaxIdleConns: 10,
		MaxOpenConns: 100,
		MaxLifetime:  time.Hour,
	}

	mysqlConn, err := app.GetMySQLConnector(mysqlConfig)
	if err != nil {
		fmt.Printf("获取MySQL连接器失败: %v\n", err)
		return
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mysqlConn.HealthCheck(ctx); err != nil {
		fmt.Printf("MySQL健康检查失败: %v\n", err)
	} else {
		fmt.Println("MySQL连接正常")
	}
}

func testRedis(app *container.Container) {
	// 获取 Redis 连接器
	redisConfig := connector.RedisConfig{
		Addr:         "127.0.0.1:6379",
		Password:     "your_redis_password",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 5,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	redisConn, err := app.GetRedisConnector(redisConfig)
	if err != nil {
		fmt.Printf("获取Redis连接器失败: %v\n", err)
		return
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisConn.HealthCheck(ctx); err != nil {
		fmt.Printf("Redis健康检查失败: %v\n", err)
	} else {
		fmt.Println("Redis连接正常")
	}
}

func testEtcd(app *container.Container) {
	// 获取 Etcd 连接器
	etcdConfig := connector.EtcdConfig{
		Endpoints:        []string{"127.0.0.1:2379"},
		Username:         "",
		Password:         "",
		Timeout:          5 * time.Second,
		KeepAliveTime:    10 * time.Second,
		KeepAliveTimeout: 3 * time.Second,
	}

	etcdConn, err := app.GetEtcdConnector(etcdConfig)
	if err != nil {
		fmt.Printf("获取Etcd连接器失败: %v\n", err)
		return
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := etcdConn.HealthCheck(ctx); err != nil {
		fmt.Printf("Etcd健康检查失败: %v\n", err)
	} else {
		fmt.Println("Etcd连接正常")
	}
}

func testNATS(app *container.Container) {
	// 获取 NATS 连接器
	natsConfig := connector.NATSConfig{
		URL:           "nats://127.0.0.1:4222",
		Name:          "demo-client",
		Username:      "",
		Password:      "",
		ReconnectWait: 2 * time.Second,
		MaxReconnects: 60,
		PingInterval:  2 * time.Minute,
		MaxPingsOut:   2,
		Timeout:       5 * time.Second,
	}

	natsConn, err := app.GetNATSConnector(natsConfig)
	if err != nil {
		fmt.Printf("获取NATS连接器失败: %v\n", err)
		return
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := natsConn.HealthCheck(ctx); err != nil {
		fmt.Printf("NATS健康检查失败: %v\n", err)
	} else {
		fmt.Println("NATS连接正常")
	}
}

func waitForShutdown(app *container.Container) {
	// 创建信号通道
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 等待信号
	sig := <-sigChan
	fmt.Printf("收到关闭信号: %s\n", sig.String())

	// 优雅关闭
	fmt.Println("开始优雅关闭...")
	if err := app.Close(); err != nil {
		fmt.Printf("关闭容器失败: %v\n", err)
	}

	fmt.Println("应用已关闭")
}
