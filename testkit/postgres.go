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

	// PostgreSQL 容器需要时间启动，使用带超时的上下文进行重试
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 重试连接，直到 PostgreSQL 完全启动
	for {
		err = conn.Connect(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			require.NoError(t, ctx.Err(), "timeout waiting for postgresql to be ready")
		case <-time.After(2 * time.Second):
			// 继续重试
		}
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewPostgreSQLDB 获取 GORM DB 实例（基于 testcontainers）
func NewPostgreSQLDB(t *testing.T) *gorm.DB {
	return NewPostgreSQLConnector(t).GetClient()
}
