// internal/connector/redis.go
package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/pkg/clog"
	pkgconnector "github.com/ceyewan/genesis/pkg/connector"
)

// redisConnector Redis连接器实现
type redisConnector struct {
	name    string
	config  pkgconnector.RedisConfig
	client  *redis.Client
	healthy bool
	mu      sync.RWMutex
	phase   int
	logger  clog.Logger
}

// NewRedisConnector 创建新的Redis连接器
func NewRedisConnector(name string, config pkgconnector.RedisConfig, logger clog.Logger) pkgconnector.RedisConnector {
	return &redisConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
		logger:  logger,
	}
}

// Connect 建立连接
func (c *redisConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已经连接且健康，直接返回
	if c.client != nil && c.healthy {
		return nil
	}

	c.logger.InfoContext(ctx, "正在建立Redis连接", clog.String("name", c.name), clog.String("addr", c.config.Addr))

	// 验证配置
	if err := c.Validate(); err != nil {
		c.logger.ErrorContext(ctx, "Redis配置验证失败", clog.String("name", c.name), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConfig, err, false)
	}

	// 创建Redis客户端
	client := redis.NewClient(&redis.Options{
		Addr:         c.config.Addr,
		Password:     c.config.Password,
		DB:           c.config.DB,
		PoolSize:     c.config.PoolSize,
		MinIdleConns: c.config.MinIdleConns,
		MaxRetries:   c.config.MaxRetries,
		DialTimeout:  c.config.DialTimeout,
		ReadTimeout:  c.config.ReadTimeout,
		WriteTimeout: c.config.WriteTimeout,
		// 禁用维护通知以避免版本兼容警告
		DisableIndentity: true,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(ctx, c.config.DialTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		c.logger.ErrorContext(ctx, "Redis连接测试失败", clog.String("name", c.name), clog.String("addr", c.config.Addr), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConnection, err, true)
	}

	c.client = client
	c.healthy = true

	c.logger.InfoContext(ctx, "Redis连接成功", clog.String("name", c.name), clog.String("addr", c.config.Addr))
	return nil
}

// Close 关闭连接
func (c *redisConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	c.logger.Info("正在关闭Redis连接", clog.String("name", c.name), clog.String("addr", c.config.Addr))

	if err := c.client.Close(); err != nil {
		c.logger.Error("关闭Redis连接失败", clog.String("name", c.name), clog.String("addr", c.config.Addr), clog.Error(err))
		return err
	}

	c.client = nil
	c.healthy = false

	c.logger.Info("Redis连接已关闭", clog.String("name", c.name), clog.String("addr", c.config.Addr))
	return nil
}

// HealthCheck 检查连接健康状态
func (c *redisConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		c.logger.WarnContext(ctx, "Redis健康检查失败：连接已关闭", clog.String("name", c.name))
		return pkgconnector.NewError(c.name, pkgconnector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx).Err(); err != nil {
		c.healthy = false
		c.logger.ErrorContext(ctx, "Redis健康检查失败：Ping失败", clog.String("name", c.name), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrHealthCheck, err, true)
	}

	c.healthy = true
	return nil
}

// IsHealthy 返回健康状态
func (c *redisConnector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Name 返回连接器名称
func (c *redisConnector) Name() string {
	return c.name
}

// GetClient 获取类型安全的客户端
func (c *redisConnector) GetClient() *redis.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// Validate 验证配置
func (c *redisConnector) Validate() error {
	return c.config.Validate()
}

// GetLogger 获取日志器
func (c *redisConnector) GetLogger() clog.Logger {
	return c.logger
}

// Reload 重载配置（可选实现）
func (c *redisConnector) Reload(ctx context.Context, newConfig pkgconnector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newRedisConfig, ok := newConfig.(*pkgconnector.RedisConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 *RedisConfig")
	}

	c.logger.InfoContext(ctx, "正在重载Redis配置", clog.String("name", c.name))

	// 关闭现有连接
	if err := c.Close(); err != nil {
		c.logger.ErrorContext(ctx, "重载Redis配置时关闭连接失败", clog.String("name", c.name), clog.Error(err))
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = *newRedisConfig
	c.mu.Unlock()

	c.logger.InfoContext(ctx, "Redis配置已重载，正在重新连接", clog.String("name", c.name))

	// 重新连接
	return c.Connect(ctx)
}

// Start 实现 Lifecycle 接口 - 启动连接器
func (c *redisConnector) Start(ctx context.Context) error {
	return c.Connect(ctx)
}

// Stop 实现 Lifecycle 接口 - 停止连接器
func (c *redisConnector) Stop(ctx context.Context) error {
	return c.Close()
}

// Phase 返回启动阶段
func (c *redisConnector) Phase() int {
	return c.phase
}
