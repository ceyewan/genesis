package connector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type etcdConnector struct {
	cfg     *EtcdConfig
	client  *clientv3.Client
	logger  clog.Logger
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewEtcd 创建 Etcd 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewEtcd(cfg *EtcdConfig, opts ...Option) (EtcdConnector, error) {
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid etcd config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &etcdConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "etcd"), clog.String("name", cfg.Name)),
	}

	return c, nil
}

// Connect 建立连接
func (c *etcdConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.client != nil {
		return nil
	}

	c.logger.Info("attempting to connect to etcd", clog.Any("endpoints", c.cfg.Endpoints))

	// 创建 Etcd 客户端配置
	clientConfig := clientv3.Config{
		Endpoints:   c.cfg.Endpoints,
		DialTimeout: c.cfg.DialTimeout,
	}

	// 设置认证
	if c.cfg.Username != "" && c.cfg.Password != "" {
		clientConfig.Username = c.cfg.Username
		clientConfig.Password = c.cfg.Password
	}

	// 创建客户端
	client, err := clientv3.New(clientConfig)
	if err != nil {
		c.logger.Error("failed to create etcd client", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "etcd connector[%s]: %v", c.cfg.Name, err)
	}

	// 测试连接
	testCtx, cancel := context.WithTimeout(ctx, c.cfg.DialTimeout)
	defer cancel()

	_, err = client.Get(testCtx, "health-check")
	// etcd v3 对于不存在的键返回空响应，不返回错误
	if err != nil {
		client.Close()
		c.logger.Error("failed to connect to etcd", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "etcd connector[%s]: %v", c.cfg.Name, err)
	}

	c.client = client
	c.healthy.Store(true)
	c.logger.Info("successfully connected to etcd", clog.Any("endpoints", c.cfg.Endpoints))
	return nil
}

// Close 关闭连接
func (c *etcdConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing etcd connection")
	c.healthy.Store(false)

	if c.client == nil {
		return nil
	}

	if err := c.client.Close(); err != nil {
		c.logger.Error("failed to close etcd connection", clog.Error(err))
		return err
	}

	c.client = nil
	c.logger.Info("etcd connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *etcdConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrClientNil, "etcd connector[%s]", c.cfg.Name)
	}

	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := client.Get(testCtx, "health-check")
	// etcd v3 对于不存在的键返回空响应，不返回错误
	if err != nil {
		c.healthy.Store(false)
		c.logger.Warn("etcd health check failed", clog.Error(err))
		return xerrors.Wrapf(ErrHealthCheck, "etcd connector[%s]: %v", c.cfg.Name, err)
	}

	c.healthy.Store(true)
	return nil
}

// IsHealthy 返回缓存的健康状态
func (c *etcdConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *etcdConnector) Name() string {
	return c.cfg.Name
}

// GetClient 返回 Etcd 客户端
func (c *etcdConnector) GetClient() *clientv3.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}

