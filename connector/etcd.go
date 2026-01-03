package connector

import (
	"context"
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
}

// NewEtcd 创建 Etcd 连接器
func NewEtcd(cfg *EtcdConfig, opts ...Option) (EtcdConnector, error) {
	cfg.setDefaults()
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

	// 创建 Etcd 客户端配置
	clientConfig := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: c.getEffectiveDialTimeout(),
	}

	// 设置认证
	if cfg.Username != "" && cfg.Password != "" {
		clientConfig.Username = cfg.Username
		clientConfig.Password = cfg.Password
	}

	// 创建客户端
	client, err := clientv3.New(clientConfig)
	if err != nil {
		return nil, xerrors.Wrapf(err, "etcd connector[%s]: connection failed", c.cfg.Name)
	}

	c.client = client
	return c, nil
}

// getEffectiveDialTimeout 获取有效的拨号超时时间
func (c *etcdConnector) getEffectiveDialTimeout() time.Duration {
	if c.cfg.DialTimeout > 0 {
		return c.cfg.DialTimeout
	}
	if c.cfg.Timeout > 0 {
		return c.cfg.Timeout
	}
	if c.cfg.ConnectTimeout > 0 {
		return c.cfg.ConnectTimeout
	}
	return 5 * time.Second
}

// Connect 建立连接
func (c *etcdConnector) Connect(ctx context.Context) error {
	c.logger.Info("attempting to connect to etcd", clog.Any("endpoints", c.cfg.Endpoints))

	// 测试连接
	testCtx, cancel := context.WithTimeout(ctx, c.getEffectiveDialTimeout())
	defer cancel()

	_, err := c.client.Get(testCtx, "health-check")
	if err != nil && !isEtcdNotFoundErr(err) {
		c.logger.Error("failed to connect to etcd", clog.Error(err))
		return xerrors.Wrapf(err, "etcd connector[%s]: connect failed", c.cfg.Name)
	}

	c.healthy.Store(true)
	c.logger.Info("successfully connected to etcd", clog.Any("endpoints", c.cfg.Endpoints))
	return nil
}

// Close 关闭连接
func (c *etcdConnector) Close() error {
	c.logger.Info("closing etcd connection")
	c.healthy.Store(false)

	if c.client != nil {
		err := c.client.Close()
		if err != nil {
			c.logger.Error("failed to close etcd connection", clog.Error(err))
			return err
		}
		c.logger.Info("etcd connection closed successfully")
	}
	return nil
}

// HealthCheck 检查连接健康状态
func (c *etcdConnector) HealthCheck(ctx context.Context) error {
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.client.Get(testCtx, "health-check")
	if err != nil && !isEtcdNotFoundErr(err) {
		c.healthy.Store(false)
		c.logger.Warn("etcd health check failed", clog.Error(err))
		return xerrors.Wrapf(err, "etcd connector[%s]: health check failed", c.cfg.Name)
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
	return c.client
}

// isEtcdNotFoundErr 检查是否是 etcd 的"未找到"错误
func isEtcdNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return err == context.DeadlineExceeded ||
		err == context.Canceled ||
		(err.Error() == "etcdserver: requested key not found")
}
