// internal/connector/nats.go
package connector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	pkgconnector "github.com/ceyewan/genesis/pkg/connector"
	"github.com/nats-io/nats.go"
)

// natsConnector NATS连接器实现
type natsConnector struct {
	name    string
	config  pkgconnector.NATSConfig
	client  *nats.Conn
	healthy bool
	mu      sync.RWMutex
	phase   int
	logger  clog.Logger
}

// NewNATSConnector 创建新的NATS连接器
func NewNATSConnector(name string, config pkgconnector.NATSConfig, logger clog.Logger) pkgconnector.NATSConnector {
	return &natsConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
		logger:  logger,
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

	c.logger.InfoContext(ctx, "正在建立NATS连接", clog.String("name", c.name), clog.String("url", c.config.URL))

	// 验证配置
	if err := c.Validate(); err != nil {
		c.logger.ErrorContext(ctx, "NATS配置验证失败", clog.String("name", c.name), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConfig, err, false)
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
				c.logger.Error("NATS连接断开", clog.String("name", c.name), clog.String("url", c.config.URL), clog.Error(err))
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			c.mu.Lock()
			c.healthy = true
			c.mu.Unlock()
			c.logger.Info("NATS连接已重连", clog.String("name", c.name), clog.String("url", c.config.URL))
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			c.mu.Lock()
			c.healthy = false
			c.mu.Unlock()
			c.logger.Info("NATS连接已关闭", clog.String("name", c.name), clog.String("url", c.config.URL))
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
		c.logger.ErrorContext(ctx, "NATS连接失败", clog.String("name", c.name), clog.String("url", c.config.URL), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConnection, err, true)
	}

	// 测试连接
	if !client.IsConnected() {
		client.Close()
		c.logger.ErrorContext(ctx, "NATS连接测试失败：未连接", clog.String("name", c.name), clog.String("url", c.config.URL))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConnection, fmt.Errorf("连接失败"), true)
	}

	c.client = client
	c.healthy = true

	c.logger.InfoContext(ctx, "NATS连接成功", clog.String("name", c.name), clog.String("url", c.config.URL))
	return nil
}

// Close 关闭连接
func (c *natsConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	c.logger.Info("正在关闭NATS连接", clog.String("name", c.name), clog.String("url", c.config.URL))

	c.client.Close()

	c.client = nil
	c.healthy = false

	c.logger.Info("NATS连接已关闭", clog.String("name", c.name), clog.String("url", c.config.URL))
	return nil
}

// HealthCheck 检查连接健康状态
func (c *natsConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		c.logger.WarnContext(ctx, "NATS健康检查失败：连接已关闭", clog.String("name", c.name))
		return pkgconnector.NewError(c.name, pkgconnector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	if !c.client.IsConnected() {
		c.healthy = false
		c.logger.ErrorContext(ctx, "NATS健康检查失败：连接已断开", clog.String("name", c.name), clog.String("url", c.config.URL))
		return pkgconnector.NewError(c.name, pkgconnector.ErrHealthCheck, fmt.Errorf("连接已断开"), true)
	}

	// 测试连接状态
	if err := c.client.FlushTimeout(5 * time.Second); err != nil {
		c.healthy = false
		c.logger.ErrorContext(ctx, "NATS健康检查失败：Flush超时", clog.String("name", c.name), clog.String("url", c.config.URL), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrHealthCheck, err, true)
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
func (c *natsConnector) Reload(ctx context.Context, newConfig pkgconnector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newNATSConfig, ok := newConfig.(pkgconnector.NATSConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 NATSConfig")
	}

	c.logger.InfoContext(ctx, "正在重载NATS配置", clog.String("name", c.name))

	// 关闭现有连接
	if err := c.Close(); err != nil {
		c.logger.ErrorContext(ctx, "重载NATS配置时关闭连接失败", clog.String("name", c.name), clog.Error(err))
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = newNATSConfig
	c.mu.Unlock()

	c.logger.InfoContext(ctx, "NATS配置已重载，正在重新连接", clog.String("name", c.name))

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

// GetLogger 获取日志器
func (c *natsConnector) GetLogger() clog.Logger {
	return c.logger
}
