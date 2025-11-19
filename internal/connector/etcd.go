// internal/connector/etcd_new.go
package connector

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/connector"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// etcdConnector Etcd连接器实现
type etcdConnector struct {
	name    string
	config  connector.EtcdConfig
	client  *clientv3.Client
	healthy bool
	mu      sync.RWMutex
	phase   int
}

// NewEtcdConnector 创建新的Etcd连接器
func NewEtcdConnector(name string, config connector.EtcdConfig) connector.EtcdConnector {
	return &etcdConnector{
		name:    name,
		config:  config,
		healthy: false,
		phase:   10, // 连接器阶段
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

	// 验证配置
	if err := c.Validate(); err != nil {
		return connector.NewError(c.name, connector.ErrConfig, err, false)
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
		return connector.NewError(c.name, connector.ErrConnection, err, true)
	}

	// 测试连接
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	if _, err := client.Status(ctx, c.config.Endpoints[0]); err != nil {
		client.Close()
		return connector.NewError(c.name, connector.ErrConnection, err, true)
	}

	c.client = client
	c.healthy = true

	return nil
}

// Close 关闭连接
func (c *etcdConnector) Close() error {
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
func (c *etcdConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.client == nil {
		return connector.NewError(c.name, connector.ErrClosed, fmt.Errorf("连接已关闭"), false)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if _, err := c.client.Status(ctx, c.config.Endpoints[0]); err != nil {
		c.healthy = false
		return connector.NewError(c.name, connector.ErrHealthCheck, err, true)
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
func (c *etcdConnector) Reload(ctx context.Context, newConfig connector.Configurable) error {
	// 验证新配置
	if err := newConfig.Validate(); err != nil {
		return err
	}

	// 类型断言
	newEtcdConfig, ok := newConfig.(connector.EtcdConfig)
	if !ok {
		return fmt.Errorf("配置类型不匹配，期望 EtcdConfig")
	}

	// 关闭现有连接
	if err := c.Close(); err != nil {
		return err
	}

	// 更新配置
	c.mu.Lock()
	c.config = newEtcdConfig
	c.mu.Unlock()

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
