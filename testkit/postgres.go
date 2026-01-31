package testkit

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"gorm.io/gorm"
)

// NewPostgreSQLContainerConfig 使用 testcontainers 创建 PostgreSQL 容器并返回配置
// 生命周期由 t.Cleanup 管理
func NewPostgreSQLContainerConfig(t *testing.T) *connector.PostgreSQLConfig {
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("genesis_db"),
		postgres.WithUsername("genesis_user"),
		postgres.WithPassword("genesis_password"),
		postgres.BasicWaitStrategies(), // 等待 PostgreSQL 完全启动
	)
	require.NoError(t, err, "failed to start PostgreSQL container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	port, err := strconv.Atoi(mappedPort.Port())
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	return &connector.PostgreSQLConfig{
		Name:            "testcontainer-postgresql",
		Host:            host,
		Port:            port,
		Username:        "genesis_user",
		Password:        "genesis_password",
		Database:        "genesis_db",
		SSLMode:         "disable",
		MaxIdleConns:    2,
		MaxOpenConns:    10,
		ConnMaxLifetime: 1 * time.Hour,
	}
}

// NewPostgreSQLConnector 获取 PostgreSQL 连接器（基于 testcontainers）
// 生命周期由 t.Cleanup 管理
func NewPostgreSQLConnector(t *testing.T) connector.PostgreSQLConnector {
	cfg := NewPostgreSQLContainerConfig(t)
	conn, err := connector.NewPostgreSQL(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create postgresql connector")

	// 容器已经通过 BasicWaitStrategies() 确保就绪，直接连接即可
	ctx := context.Background()
	err = conn.Connect(ctx)
	require.NoError(t, err, "failed to connect to postgresql")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewPostgreSQLDB 获取 GORM DB 实例（基于 testcontainers）
func NewPostgreSQLDB(t *testing.T) *gorm.DB {
	return NewPostgreSQLConnector(t).GetClient()
}
