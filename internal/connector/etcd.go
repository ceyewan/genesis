// internal/connector/etcd.go
package connector

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/ceyewan/genesis/pkg/clog"
	pkgconnector "github.com/ceyewan/genesis/pkg/connector"
)

// etcdConnector Etcd连接器实现
type etcdConnector struct {
	name    string
	config  pkgconnector.EtcdConfig
	client  *clientv3.Client
	healthy bool
	mu      sync.RWMutex
	phase   int
	logger  clog.Logger
}

// NewEtcdConnector 创建新的Etcd连接器
func NewEtcdConnector(name string, config pkgconnector.EtcdConfig, logger clog.Logger) pkgconnector.EtcdConnector {
	return &etcdConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
		logger:  logger,
	}
}

// Connect 建立连接
func (c *etcdConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已经连接且健康，直接返回
	if c.client != nil && c.healthy {
		return nil
	}

	c.logger.InfoContext(ctx, "正在建立Etcd连接", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)))

	// 验证配置
	if err := c.Validate(); err != nil {
		c.logger.ErrorContext(ctx, "Etcd配置验证失败", clog.String("name", c.name), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConfig, err, false)
	}

	// 创建Etcd客户端配置
	clientConfig := clientv3.Config{
		Endpoints:            c.config.Endpoints,
		Username:             c.config.Username,
		Password:             c.config.Password,
		DialTimeout:          c.config.Timeout,
		DialKeepAliveTime:    c.config.KeepAliveTime,
		DialKeepAliveTimeout: c.config.KeepAliveTimeout,
	}

	// 创建客户端
	client, err := clientv3.New(clientConfig)
	if err != nil {
		c.logger.ErrorContext(ctx, "Etcd客户端创建失败", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConnection, err, true)
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	if _, err := client.Status(ctx, c.config.Endpoints[0]); err != nil {
		client.Close()
		c.logger.ErrorContext(ctx, "Etcd连接测试失败", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrConnection, err, true)
	}

	c.client = client
	c.healthy = true

	c.logger.InfoContext(ctx, "Etcd连接成功", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)))
	return nil
}

// Close 关闭连接
func (c *etcdConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client == nil {
		return nil
	}

	c.logger.Info("正在关闭Etcd连接", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)))

	if err := c.client.Close(); err != nil {
		c.logger.Error("关闭Etcd连接失败", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)), clog.Error(err))
		return err
	}

	c.client = nil
	c.healthy = false

	c.logger.Info("Etcd连接已关闭", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)))
	return nil
}

// HealthCheck 检查连接健康状态
func (c *etcdConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		c.logger.WarnContext(ctx, "Etcd健康检查失败：连接已关闭", clog.String("name", c.name))
		return pkgconnector.NewError(c.name, pkgconnector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := c.client.Status(ctx, c.config.Endpoints[0]); err != nil {
		c.healthy = false
		c.logger.ErrorContext(ctx, "Etcd健康检查失败：Status失败", clog.String("name", c.name), clog.String("endpoints", fmt.Sprintf("%v", c.config.Endpoints)), clog.Error(err))
		return pkgconnector.NewError(c.name, pkgconnector.ErrHealthCheck, err, true)
	}

	c.healthy = true
	return nil
}

// IsHealthy 返回健康状态
func (c *etcdConnector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

// Name 返回连接器名称
func (c *etcdConnector) Name() string {
	return c.name
}

// GetClient 获取类型安全的客户端
func (c *etcdConnector) GetClient() *clientv3.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

// Validate 验证配置
func (c *etcdConnector) Validate() error {
	return c.config.Validate()
}

// Reload 重载配置（可选实现）
func (c *etcdConnector) Reload(ctx context.Context, newConfig pkgconnector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newEtcdConfig, ok := newConfig.(*pkgconnector.EtcdConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 *EtcdConfig")
	}

	c.logger.InfoContext(ctx, "正在重载Etcd配置", clog.String("name", c.name))

	// 关闭现有连接
	if err := c.Close(); err != nil {
		c.logger.ErrorContext(ctx, "重载Etcd配置时关闭连接失败", clog.String("name", c.name), clog.Error(err))
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = *newEtcdConfig
	c.mu.Unlock()

	c.logger.InfoContext(ctx, "Etcd配置已重载，正在重新连接", clog.String("name", c.name))

	// 重新连接
	return c.Connect(ctx)
}

// GetEndpoints 获取端点列表（排序后）
func (c *etcdConnector) GetEndpoints() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	endpoints := make([]string, len(c.config.Endpoints))
	copy(endpoints, c.config.Endpoints)
	sort.Strings(endpoints)
	return endpoints
}

// GetLogger 获取日志器
func (c *etcdConnector) GetLogger() clog.Logger {
	return c.logger
}

// Start 实现 Lifecycle 接口 - 启动连接器
func (c *etcdConnector) Start(ctx context.Context) error {
	return c.Connect(ctx)
}

// Stop 实现 Lifecycle 接口 - 停止连接器
func (c *etcdConnector) Stop(ctx context.Context) error {
	return c.Close()
}

// Phase 返回启动阶段
func (c *etcdConnector) Phase() int {
	return c.phase
}
