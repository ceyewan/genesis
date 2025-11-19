// internal/connector/redis_new.go
package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/redis/go-redis/v9"
)

// redisConnector Redis连接器实现
type redisConnector struct {
	name    string
	config  connector.RedisConfig
	client  *redis.Client
	healthy bool
	mu      sync.RWMutex
	phase   int
}

// NewRedisConnector 创建新的Redis连接器
func NewRedisConnector(name string, config connector.RedisConfig) connector.RedisConnector {
	return &redisConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
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

	// 验证配置
	if err := c.Validate(); err != nil {
		return connector.NewError(c.name, connector.ErrConfig, err, false)
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
		return connector.NewError(c.name, connector.ErrConnection, err, true)
	}

	c.client = client
	c.healthy = true

	return nil
}

// Close 关闭连接
func (c *redisConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	if err := c.client.Close(); err != nil {
		return err
	}

	c.client = nil
	c.healthy = false

	return nil
}

// HealthCheck 检查连接健康状态
func (c *redisConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return connector.NewError(c.name, connector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := c.client.Ping(ctx).Err(); err != nil {
		c.healthy = false
		return connector.NewError(c.name, connector.ErrHealthCheck, err, true)
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

// Reload 重载配置（可选实现）
func (c *redisConnector) Reload(ctx context.Context, newConfig connector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newRedisConfig, ok := newConfig.(connector.RedisConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 RedisConfig")
	}

	// 关闭现有连接
	if err := c.Close(); err != nil {
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = newRedisConfig
	c.mu.Unlock()

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
