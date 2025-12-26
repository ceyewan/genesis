package testkit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ceyewan/genesis/connector"
	"gorm.io/gorm"
)

// GetMySQLConfig 返回 MySQL 测试配置
// 默认连接 localhost:3306
// 环境变量遵循 .env 文件定义：
// MYSQL_USER
// MYSQL_PASSWORD
// MYSQL_DATABASE
func GetMySQLConfig() *connector.MySQLConfig {
	user := os.Getenv("MYSQL_USER")
	if user == "" {
		user = "genesis_user"
	}
	password := os.Getenv("MYSQL_PASSWORD")
	if password == "" {
		password = "genesis_password"
	}
	dbName := os.Getenv("MYSQL_DATABASE")
	if dbName == "" {
		dbName = "genesis_db"
	}

	return &connector.MySQLConfig{
		Name:            "test-mysql",
		Host:            "localhost",
		Port:            3306,
		Username:        user,
		Password:        password,
		Database:        dbName,
		MaxIdleConns:    2,
		MaxOpenConns:    10,
		ConnMaxLifetime: 1 * time.Hour,
	}
}

// GetMySQLConnector 获取 MySQL 连接器
func GetMySQLConnector(t *testing.T) connector.MySQLConnector {
	cfg := GetMySQLConfig()
	conn, err := connector.NewMySQL(cfg, connector.WithLogger(NewLogger()))
	if err != nil {
		t.Fatalf("failed to create mysql connector: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		t.Fatalf("failed to connect to mysql: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
	})

	return conn
}

// GetMySQLDB 获取 GORM DB 实例
func GetMySQLDB(t *testing.T) *gorm.DB {
	return GetMySQLConnector(t).GetClient()
}
