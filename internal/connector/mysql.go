// internal/connector/mysql.go
package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// mysqlConnector MySQL连接器实现
type mysqlConnector struct {
	name    string
	config  connector.MySQLConfig
	client  *gorm.DB
	healthy bool
	mu      sync.RWMutex
	phase   int
}

// NewMySQLConnector 创建新的MySQL连接器
func NewMySQLConnector(name string, config connector.MySQLConfig) connector.MySQLConnector {
	return &mysqlConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
	}
}

// Connect 建立连接
func (c *mysqlConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已经连接且健康，直接返回
	if c.client != nil && c.healthy {
		return nil
	}

	// 验证配置
	if err := c.Validate(); err != nil {
		return connector.NewError(c.name, connector.ErrConfig, err, false)
	}

	// 构建DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local&timeout=%s",
		c.config.Username,
		c.config.Password,
		c.config.Host,
		c.config.Port,
		c.config.Database,
		c.config.Charset,
		c.config.Timeout,
	)

	// 创建数据库连接
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return connector.NewError(c.name, connector.ErrConnection, err, true)
	}

	// 获取底层数据库连接
	sqlDB, err := db.DB()
	if err != nil {
		return connector.NewError(c.name, connector.ErrConnection, err, false)
	}

	// 设置连接池参数
	if c.config.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(c.config.MaxIdleConns)
	}
	if c.config.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(c.config.MaxOpenConns)
	}
	if c.config.MaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(c.config.MaxLifetime)
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return connector.NewError(c.name, connector.ErrConnection, err, true)
	}

	c.client = db
	c.healthy = true

	return nil
}

// Close 关闭连接
func (c *mysqlConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	sqlDB, err := c.client.DB()
	if err != nil {
		return err
	}

	if err := sqlDB.Close(); err != nil {
		return err
	}

	c.client = nil
	c.healthy = false

	return nil
}

// HealthCheck 检查连接健康状态
func (c *mysqlConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return connector.NewError(c.name, connector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	sqlDB, err := c.client.DB()
	if err != nil {
		c.healthy = false
		return connector.NewError(c.name, connector.ErrHealthCheck, err, true)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		c.healthy = false
		return connector.NewError(c.name, connector.ErrHealthCheck, err, true)
	}

	c.healthy = true
	return nil
}

// IsHealthy 返回健康状态
func (c *mysqlConnector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Name 返回连接器名称
func (c *mysqlConnector) Name() string {
	return c.name
}

// GetClient 获取类型安全的客户端
func (c *mysqlConnector) GetClient() *gorm.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// Validate 验证配置
func (c *mysqlConnector) Validate() error {
	if c.config.Host == "" {
		return fmt.Errorf("主机地址不能为空")
	}
	if c.config.Port <= 0 {
		c.config.Port = 3306
	}
	if c.config.Username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	if c.config.Database == "" {
		return fmt.Errorf("数据库名不能为空")
	}
	if c.config.Charset == "" {
		c.config.Charset = "utf8mb4"
	}
	if c.config.Timeout == 0 {
		c.config.Timeout = 10 * time.Second
	}
	if c.config.MaxIdleConns <= 0 {
		c.config.MaxIdleConns = 10
	}
	if c.config.MaxOpenConns <= 0 {
		c.config.MaxOpenConns = 100
	}
	if c.config.MaxLifetime == 0 {
		c.config.MaxLifetime = time.Hour
	}

	return nil
}

// Reload 重载配置（可选实现）
func (c *mysqlConnector) Reload(ctx context.Context, newConfig connector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newMySQLConfig, ok := newConfig.(connector.MySQLConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 MySQLConfig")
	}

	// 关闭现有连接
	if err := c.Close(); err != nil {
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = newMySQLConfig
	c.mu.Unlock()

	// 重新连接
	return c.Connect(ctx)
}

// Start 实现 Lifecycle 接口 - 启动连接器
func (c *mysqlConnector) Start(ctx context.Context) error {
	return c.Connect(ctx)
}

// Stop 实现 Lifecycle 接口 - 停止连接器
func (c *mysqlConnector) Stop(ctx context.Context) error {
	return c.Close()
}

// Phase 返回启动阶段
func (c *mysqlConnector) Phase() int {
	return c.phase
}
