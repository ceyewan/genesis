package connector

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/nats-io/nats.go"
)

type natsConnector struct {
	cfg     *NATSConfig
	conn    *nats.Conn
	connect func(string, ...nats.Option) (*nats.Conn, error)
	logger  clog.Logger
	metrics *connectorTelemetry
	healthy atomic.Bool
	closing atomic.Bool
	mu      sync.RWMutex
}

// NewNATS 创建 NATS 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewNATS(cfg *NATSConfig, opts ...Option) (NATSConnector, error) {
	if cfg == nil {
		return nil, xerrors.Wrap(ErrConfig, "nats config is nil")
	}
	cfgCopy := *cfg
	cfg = &cfgCopy
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid nats config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &natsConnector{
		cfg:     cfg,
		connect: nats.Connect,
		logger:  opt.logger.With(clog.String("connector", "nats"), clog.String("name", cfg.Name)),
		metrics: newConnectorTelemetry(opt.meter, "nats", cfg.Name),
	}

	return c, nil
}

// Connect 建立连接
func (c *natsConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.conn != nil {
		return nil
	}
	c.closing.Store(false)

	c.logger.Info("attempting to connect to nats", clog.String("url", c.cfg.URL))

	// 创建 NATS 连接选项
	natsOpts := []nats.Option{
		nats.Name(c.cfg.Name),
		nats.ReconnectWait(c.cfg.ReconnectWait),
		nats.MaxReconnects(c.cfg.MaxReconnects),
		nats.PingInterval(c.cfg.PingInterval),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			c.healthy.Store(false)
			if c.closing.Load() {
				return
			}
			c.logger.Warn("nats disconnected", clog.Error(err))
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			c.healthy.Store(true)
			c.metrics.observeReconnect()
			c.logger.Info("nats reconnected", clog.String("url", nc.ConnectedUrl()))
		}),
		nats.ClosedHandler(func(nc *nats.Conn) {
			c.healthy.Store(false)
			if c.closing.Load() {
				return
			}
			c.logger.Warn("nats connection closed", clog.Error(nc.LastError()))
		}),
	}

	// 添加认证
	if c.cfg.Username != "" && c.cfg.Password != "" {
		natsOpts = append(natsOpts, nats.UserInfo(c.cfg.Username, c.cfg.Password))
	}
	if c.cfg.Token != "" {
		natsOpts = append(natsOpts, nats.Token(c.cfg.Token))
	}

	// nats.Connect itself does not accept a context. Run the bounded attempt
	// behind a context-aware handoff and use the shorter caller deadline as the
	// NATS timeout. A connection that completes after cancellation is closed by
	// the attempt goroutine instead of being leaked.
	conn, err := c.connectWithContext(ctx, natsOpts...)
	if err != nil {
		c.logger.Error("failed to connect to nats", clog.Error(err), clog.String("url", c.cfg.URL))
		return wrapConnectorCause(ErrConnection, err, "nats connector[%s]", c.cfg.Name)
	}

	c.conn = conn
	c.healthy.Store(true)
	c.logger.Info("successfully connected to nats", clog.String("url", c.cfg.URL))

	return nil
}

type natsConnectResult struct {
	conn *nats.Conn
	err  error
}

func (c *natsConnector) connectWithContext(ctx context.Context, opts ...nats.Option) (*nats.Conn, error) {
	if ctx == nil {
		return nil, xerrors.New("nats connect context is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	timeout := c.cfg.ConnectTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		timeout = min(timeout, remaining)
	}
	opts = append(opts, nats.Timeout(timeout))

	resultCh := make(chan natsConnectResult)
	abandoned := make(chan struct{})
	go func() {
		conn, err := c.connect(c.cfg.URL, opts...)
		select {
		case resultCh <- natsConnectResult{conn: conn, err: err}:
		case <-abandoned:
			if conn != nil {
				conn.Close()
			}
		}
	}()

	select {
	case result := <-resultCh:
		close(abandoned)
		if result.err != nil && result.conn != nil {
			result.conn.Close()
			result.conn = nil
		}
		return result.conn, result.err
	case <-ctx.Done():
		close(abandoned)
		return nil, ctx.Err()
	}
}

// Close 关闭连接
func (c *natsConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closing.Store(true)
	c.healthy.Store(false)

	if c.conn == nil {
		return nil
	}

	c.logger.Info("closing nats connection", clog.String("url", c.cfg.URL))

	// Drain 确保消息完全处理后再关闭（仅在已连接状态下）
	if c.conn.Status() == nats.CONNECTED {
		c.logger.Debug("draining nats connection before close")
		if err := c.conn.Drain(); err != nil {
			c.logger.Warn("failed to drain nats connection", clog.Error(err))
		}
	}

	c.conn.Close()
	c.conn = nil
	c.logger.Info("nats connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *natsConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		c.healthy.Store(false)
		return c.metrics.observeHealth(ctx, xerrors.Wrapf(ErrClientNil, "nats connector[%s]", c.cfg.Name))
	}

	// 检查连接状态
	status := conn.Status()
	if status != nats.CONNECTED {
		c.healthy.Store(false)
		return c.metrics.observeHealth(ctx, xerrors.Wrapf(ErrHealthCheck, "nats connector[%s]: connection status: %s", c.cfg.Name, status.String()))
	}

	c.healthy.Store(true)
	return c.metrics.observeHealth(ctx, nil)
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
