package connector

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/nats-io/nats.go"
)

type natsConnector struct {
	cfg     *NATSConfig
	conn    *nats.Conn
	logger  clog.Logger
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewNATS 创建 NATS 连接器
func NewNATS(cfg *NATSConfig, opts ...Option) (NATSConnector, error) {
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid nats config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &natsConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "nats"), clog.String("name", cfg.Name)),
	}

	return c, nil
}

// Connect 建立连接
func (c *natsConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("attempting to connect to nats", clog.String("url", c.cfg.URL))

	// 创建 NATS 连接选项
	natsOpts := []nats.Option{
		nats.Name(c.cfg.Name),
		nats.ReconnectWait(c.cfg.ReconnectWait),
		nats.MaxReconnects(c.cfg.MaxReconnects),
		nats.PingInterval(c.cfg.PingInterval),
		nats.Timeout(c.cfg.Timeout),
	}

	// 添加认证
	if c.cfg.Username != "" && c.cfg.Password != "" {
		natsOpts = append(natsOpts, nats.UserInfo(c.cfg.Username, c.cfg.Password))
	}
	if c.cfg.Token != "" {
		natsOpts = append(natsOpts, nats.Token(c.cfg.Token))
	}

	// 建立连接
	conn, err := nats.Connect(c.cfg.URL, natsOpts...)
	if err != nil {
		c.logger.Error("failed to connect to nats", clog.Error(err), clog.String("url", c.cfg.URL))
		return xerrors.Wrapf(err, "nats connector[%s]: connection failed", c.cfg.Name)
	}

	c.conn = conn
	c.healthy.Store(true)
	c.logger.Info("successfully connected to nats", clog.String("url", c.cfg.URL))

	return nil
}

// Close 关闭连接
func (c *natsConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing nats connection", clog.String("url", c.cfg.URL))
	c.healthy.Store(false)

	if c.conn != nil {
		c.conn.Close()
		c.logger.Info("nats connection closed successfully")
	}
	return nil
}

// HealthCheck 检查连接健康状态
func (c *natsConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrConnection, "nats connector[%s]: connection is nil", c.cfg.Name)
	}

	// 检查连接状态
	status := conn.Status()
	if status == nats.CLOSED || status == nats.RECONNECTING {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrHealthCheck, "nats connector[%s]: connection status: %s", c.cfg.Name, status.String())
	}

	c.healthy.Store(true)
	return nil
}

// IsHealthy 返回缓存的健康状态
func (c *natsConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *natsConnector) Name() string {
	return c.cfg.Name
}

// GetClient 返回 NATS 连接
func (c *natsConnector) GetClient() *nats.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}
