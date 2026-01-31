package testkit

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	rediscontainer "github.com/testcontainers/testcontainers-go/modules/redis"
)

// NewRedisContainerConfig 使用 testcontainers 创建 Redis 容器并返回配置
// 生命周期由 t.Cleanup 管理
func NewRedisContainerConfig(t *testing.T) *connector.RedisConfig {
	ctx := context.Background()

	container, err := rediscontainer.Run(ctx, "redis:7.2-alpine")
	require.NoError(t, err, "failed to start Redis container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "6379")
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	return &connector.RedisConfig{
		Name:         "testcontainer-redis",
		Addr:         host + ":" + mappedPort.Port(),
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// NewRedisContainerConnector 使用 testcontainers 创建并连接 Redis 连接器
// 生命周期由 t.Cleanup 管理
func NewRedisContainerConnector(t *testing.T) connector.RedisConnector {
	cfg := NewRedisContainerConfig(t)

	conn, err := connector.NewRedis(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create redis connector")

	err = conn.Connect(context.Background())
	require.NoError(t, err, "failed to connect to redis")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewRedisContainerClient 使用 testcontainers 创建并返回原生 Redis 客户端
// 生命周期由 t.Cleanup 管理
func NewRedisContainerClient(t *testing.T) *redis.Client {
	return NewRedisContainerConnector(t).GetClient()
}
