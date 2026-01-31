package testkit

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/connector"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// NewSQLiteContainerConfig 返回 SQLite 内存数据库配置
// 默认使用内存数据库，测试结束后自动清理
func NewSQLiteConfig() *connector.SQLiteConfig {
	return &connector.SQLiteConfig{
		Path: "file::memory:?cache=shared",
	}
}

// NewSQLiteConnector 获取 SQLite 连接器（内存数据库）
// 生命周期由 t.Cleanup 管理
func NewSQLiteConnector(t *testing.T) connector.SQLiteConnector {
	cfg := NewSQLiteConfig()
	conn, err := connector.NewSQLite(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create sqlite connector")

	err = conn.Connect(context.Background())
	require.NoError(t, err, "failed to connect to sqlite")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// NewSQLiteDB 获取 GORM DB 实例（内存数据库）
func NewSQLiteDB(t *testing.T) *gorm.DB {
	return NewSQLiteConnector(t).GetClient()
}

// NewPersistentSQLiteConfig 返回持久化 SQLite 测试配置
// 用于需要文件持久化的测试场景，数据库文件存储在 t.TempDir() 中
func NewPersistentSQLiteConfig(t *testing.T) *connector.SQLiteConfig {
	return &connector.SQLiteConfig{
		Path: t.TempDir() + "/test.db",
	}
}

// NewPersistentSQLiteConnector 获取持久化 SQLite 连接器
// 数据库文件存储在临时目录中，测试结束后自动清理
// 生命周期由 t.Cleanup 管理
func NewPersistentSQLiteConnector(t *testing.T) connector.SQLiteConnector {
	cfg := NewPersistentSQLiteConfig(t)
	conn, err := connector.NewSQLite(cfg, connector.WithLogger(NewLogger()))
	require.NoError(t, err, "failed to create sqlite connector")

	err = conn.Connect(context.Background())
	require.NoError(t, err, "failed to connect to sqlite")

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}
