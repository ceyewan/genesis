// internal/connector/nats.go
package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/nats-io/nats.go"
)

// natsConnector NATS连接器实现
type natsConnector struct {
	name    string
	config  connector.NATSConfig
	client  *nats.Conn
	healthy bool
	mu      sync.RWMutex
	phase   int
}

// NewNATSConnector 创建新的NATS连接器
func NewNATSConnector(name string, config connector.NATSConfig) connector.NATSConnector {
	return &natsConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
	}
}

// Connect 建立连接
func (c *natsConnector) Connect(ctx context.Context) error {
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

	// 创建NATS连接选项
	opts := []nats.Option{
		nats.Name(c.config.Name),
		nats.ReconnectWait(c.config.ReconnectWait),
		nats.MaxReconnects(c.config.MaxReconnects),
		nats.PingInterval(c.config.PingInterval),
		nats.MaxPingsOutstanding(c.config.MaxPingsOut),
		nats.Timeout(c.config.Timeout),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				c.mu.Lock()
				c.healthy = false
				c.mu.Unlock()
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			c.mu.Lock()
			c.healthy = true
			c.mu.Unlock()
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			c.mu.Lock()
			c.healthy = false
			c.mu.Unlock()
		}),
	}

	// 添加认证信息
	if c.config.Username != "" && c.config.Password != "" {
		opts = append(opts, nats.UserInfo(c.config.Username, c.config.Password))
	}
	if c.config.Token != "" {
		opts = append(opts, nats.Token(c.config.Token))
	}

	// 创建连接
	client, err := nats.Connect(c.config.URL, opts...)
	if err != nil {
		return connector.NewError(c.name, connector.ErrConnection, err, true)
	}

	// 测试连接
	if !client.IsConnected() {
		client.Close()
		return connector.NewError(c.name, connector.ErrConnection, fmt.Errorf("连接失败"), true)
	}

	c.client = client
	c.healthy = true

	return nil
}

// Close 关闭连接
func (c *natsConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	c.client.Close()

	c.client = nil
	c.healthy = false

	return nil
}

// HealthCheck 检查连接健康状态
func (c *natsConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return connector.NewError(c.name, connector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	if !c.client.IsConnected() {
		c.healthy = false
		return connector.NewError(c.name, connector.ErrHealthCheck, fmt.Errorf("连接已断开"), true)
	}

	// 测试连接状态
	if err := c.client.FlushTimeout(5 * time.Second); err != nil {
		c.healthy = false
		return connector.NewError(c.name, connector.ErrHealthCheck, err, true)
	}

	c.healthy = true
	return nil
}

// IsHealthy 返回健康状态
func (c *natsConnector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Name 返回连接器名称
func (c *natsConnector) Name() string {
	return c.name
}

// GetClient 获取类型安全的客户端
func (c *natsConnector) GetClient() *nats.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// Validate 验证配置
func (c *natsConnector) Validate() error {
	return c.config.Validate()
}

// Reload 重载配置（可选实现）
func (c *natsConnector) Reload(ctx context.Context, newConfig connector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newNATSConfig, ok := newConfig.(connector.NATSConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 NATSConfig")
	}

	// 关闭现有连接
	if err := c.Close(); err != nil {
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = newNATSConfig
	c.mu.Unlock()

	// 重新连接
	return c.Connect(ctx)
}

// Start 实现 Lifecycle 接口 - 启动连接器
func (c *natsConnector) Start(ctx context.Context) error {
	return c.Connect(ctx)
}

// Stop 实现 Lifecycle 接口 - 停止连接器
func (c *natsConnector) Stop(ctx context.Context) error {
	return c.Close()
}

// Phase 返回启动阶段
func (c *natsConnector) Phase() int {
	return c.phase
}

// GetStats 获取连接统计信息
func (c *natsConnector) GetStats() nats.Statistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return nats.Statistics{}
	}

	return c.client.Stats()
}
