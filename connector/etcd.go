package connector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
	clientv3 "go.etcd.io/etcd/client/v3"
)

type etcdConnector struct {
	cfg                   *EtcdConfig
	client                *clientv3.Client
	logger                clog.Logger
	meter                 metrics.Meter
	healthy               atomic.Bool
	mu                    sync.RWMutex
	totalConnections      metrics.Counter
	successfulConnections metrics.Counter
	failedConnections     metrics.Counter
	activeConnections     metrics.Gauge
}

// NewEtcd 创建 Etcd 连接器
func NewEtcd(cfg *EtcdConfig, opts ...Option) (EtcdConnector, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid etcd config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}

	c := &etcdConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "etcd"), clog.String("name", cfg.Name)),
		meter:  opt.meter,
	}

	// 创建简化指标
	if c.meter != nil {
		var err error
		c.totalConnections, err = c.meter.Counter(
			"connector_etcd_total_connections",
			"Total number of Etcd connection attempts",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create total connections counter")
		}

		c.successfulConnections, err = c.meter.Counter(
			"connector_etcd_successful_connections",
			"Number of successful Etcd connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create successful connections counter")
		}

		c.failedConnections, err = c.meter.Counter(
			"connector_etcd_failed_connections",
			"Number of failed Etcd connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create failed connections counter")
		}

		c.activeConnections, err = c.meter.Gauge(
			"connector_etcd_active_connections",
			"Number of active Etcd connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create active connections gauge")
		}
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
	c.mu.Lock()
	defer c.mu.Unlock()

	// 记录总连接尝试
	if c.totalConnections != nil {
		c.totalConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
	}

	c.logger.Info("attempting to connect to etcd", clog.Any("endpoints", c.cfg.Endpoints))

	// 测试连接
	testCtx, cancel := context.WithTimeout(ctx, c.getEffectiveDialTimeout())
	defer cancel()

	_, err := c.client.Get(testCtx, "health-check")
	if err != nil && !isEtcdNotFoundErr(err) {
		// 记录失败连接
		if c.failedConnections != nil {
			c.failedConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
		}
		c.logger.Error("failed to connect to etcd", clog.Error(err))
		return xerrors.Wrapf(err, "etcd connector[%s]: connect failed", c.cfg.Name)
	}

	// 记录成功连接
	if c.successfulConnections != nil {
		c.successfulConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
	}
	if c.activeConnections != nil {
		c.activeConnections.Set(ctx, float64(1), metrics.L("connector", c.cfg.Name))
	}

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

	// 减少活跃连接数
	if c.activeConnections != nil {
		c.activeConnections.Set(context.Background(), float64(0), metrics.L("connector", c.cfg.Name))
	}

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
