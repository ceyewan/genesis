package testkit

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/mysql"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/connector"
)

// NewMySQLContainerConfig 使用 testcontainers 创建 MySQL 容器并返回配置
// 生命周期由 t.Cleanup 管理
func NewMySQLContainerConfig(t *testing.T) *connector.MySQLConfig {
	ctx := context.Background()

	container, err := mysql.Run(ctx,
		"mysql:8.0",
		mysql.WithDatabase("genesis_db"),
		mysql.WithUsername("genesis_user"),
		mysql.WithPassword("genesis_password"),
	)
	require.NoError(t, err, "failed to start MySQL container")

	host, err := container.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := container.MappedPort(ctx, "3306")
	require.NoError(t, err)

	port, err := strconv.Atoi(mappedPort.Port())
	require.NoError(t, err)

	// 注册 cleanup
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	return &connector.MySQLConfig{
		Name:            "testcontainer-mysql",
		Host:            host,
		Port:            port,
		Username:        "genesis_user",
		Password:        "genesis_password",
		Database:        "genesis_db",
		MaxIdleConns:    2,
		MaxOpenConns:    10,
		ConnMaxLifetime: 1 * time.Hour,
	}
}

// NewMySQLConnector 获取 MySQL 连接器（基于 testcontainers）
// 生命周期由 t.Cleanup 管理
func NewMySQLConnector(t *testing.T) connector.MySQLConnector {
	cfg := NewMySQLContainerConfig(t)
	conn, err := connector.NewMySQL(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create mysql connector")

	// MySQL 容器需要时间启动，使用带超时的上下文进行重试
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 重试连接，直到 MySQL 完全启动
	for {
		err = conn.Connect(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			require.NoError(t, ctx.Err(), "timeout waiting for mysql to be ready")
		case <-time.After(2 * time.Second):
			// 继续重试
		}
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewMySQLDB 获取 GORM DB 实例（基于 testcontainers）
func NewMySQLDB(t *testing.T) *gorm.DB {
	return NewMySQLConnector(t).GetClient()
}
