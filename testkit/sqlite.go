package testkit

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/connector"
	"gorm.io/gorm"
)

// GetSQLiteConfig 返回 SQLite 测试配置
// 默认使用内存数据库，测试结束后自动清理
func GetSQLiteConfig() *connector.SQLiteConfig {
	return &connector.SQLiteConfig{
		Path: "file::memory:?cache=shared",
	}
}

// GetSQLiteConnector 获取 SQLite 连接器
func GetSQLiteConnector(t *testing.T) connector.SQLiteConnector {
	cfg := GetSQLiteConfig()
	conn, err := connector.NewSQLite(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create sqlite connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to sqlite: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// GetSQLiteDB 获取 GORM DB 实例
func GetSQLiteDB(t *testing.T) *gorm.DB {
	return GetSQLiteConnector(t).GetClient()
}

// GetPersistentSQLiteConfig 返回持久化 SQLite 测试配置
// 用于需要文件持久化的测试场景
func GetPersistentSQLiteConfig(t *testing.T) *connector.SQLiteConfig {
	return &connector.SQLiteConfig{
		Path: t.TempDir() + "/test.db",
	}
}

// GetPersistentSQLiteConnector 获取持久化 SQLite 连接器
// 数据库文件存储在临时目录中，测试结束后自动清理
func GetPersistentSQLiteConnector(t *testing.T) connector.SQLiteConnector {
	cfg := GetPersistentSQLiteConfig(t)
	conn, err := connector.NewSQLite(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create sqlite connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to sqlite: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}
