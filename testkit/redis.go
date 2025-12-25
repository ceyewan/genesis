package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/redis/go-redis/v9"
)

// GetRedisConfig 返回 Redis 测试配置
// 默认连接 localhost:6379，可通过 GENESIS_TEST_REDIS_ADDR 环境变量覆盖
func GetRedisConfig() *connector.RedisConfig {
	return &connector.RedisConfig{
		Name:         "test-redis",
		Addr:         "localhost:6379",
		DB:           1, // 使用 DB 1 避免与默认的 DB 0 冲突
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// GetRedisConnector 获取 Redis 连接器
func GetRedisConnector(t *testing.T) connector.RedisConnector {
	cfg := GetRedisConfig()
	conn, err := connector.NewRedis(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create redis connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to redis: %v", err)
	}
	// 注册清理函数
	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// GetRedisClient 获取原生 Redis 客户端
func GetRedisClient(t *testing.T) *redis.Client {
	return GetRedisConnector(t).GetClient()
}

// FlushRedis 清空 Redis 数据库（慎用！）
func FlushRedis(t *testing.T, client *redis.Client) {
	if err := client.FlushDB(context.Background()).Err(); err != nil {
		t.Fatalf("failed to flush redis: %v", err)
	}
}
