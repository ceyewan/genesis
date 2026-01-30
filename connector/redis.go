package connector

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

type redisConnector struct {
	cfg     *RedisConfig
	client  *redis.Client
	logger  clog.Logger
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewRedis 创建 Redis 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid redis config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &redisConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "redis"), clog.String("name", cfg.Name)),
	}

	return c, nil
}

// Connect 建立连接
func (c *redisConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.client != nil {
		return nil
	}

	c.logger.Info("attempting to connect to redis", clog.String("addr", c.cfg.Addr))

	// 创建 Redis 客户端
	client := redis.NewClient(&redis.Options{
		Addr:         c.cfg.Addr,
		Password:     c.cfg.Password,
		DB:           c.cfg.DB,
		PoolSize:     c.cfg.PoolSize,
		MinIdleConns: c.cfg.MinIdleConns,
		DialTimeout:  c.cfg.DialTimeout,
		ReadTimeout:  c.cfg.ReadTimeout,
		WriteTimeout: c.cfg.WriteTimeout,
		MaintNotificationsConfig: &maintnotifications.Config{
			Mode: maintnotifications.ModeDisabled,
		},
	})

	// 启用 Tracing
	if c.cfg.EnableTracing {
		if err := redisotel.InstrumentTracing(client); err != nil {
			client.Close()
			return xerrors.Wrapf(ErrConnection, "redis connector[%s]: enable tracing failed: %v", c.cfg.Name, err)
		}
	}

	// 测试连接
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		c.logger.Error("failed to connect to redis", clog.Error(err), clog.String("addr", c.cfg.Addr))
		return xerrors.Wrapf(ErrConnection, "redis connector[%s]: ping failed: %v", c.cfg.Name, err)
	}

	c.client = client
	c.healthy.Store(true)
	c.logger.Info("successfully connected to redis", clog.String("addr", c.cfg.Addr))

	return nil
}

// Close 关闭连接
func (c *redisConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing redis connection", clog.String("addr", c.cfg.Addr))
	c.healthy.Store(false)

	if c.client == nil {
		return nil
	}

	if err := c.client.Close(); err != nil {
		c.logger.Error("failed to close redis connection", clog.Error(err))
		return err
	}

	c.client = nil
	c.logger.Info("redis connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *redisConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	client := c.client
	c.mu.RUnlock()

	if client == nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrClientNil, "redis connector[%s]", c.cfg.Name)
	}

	if err := client.Ping(ctx).Err(); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("redis health check failed", clog.Error(err))
		return xerrors.Wrapf(ErrHealthCheck, "redis connector[%s]: %v", c.cfg.Name, err)
	}

	c.healthy.Store(true)
	return nil
}

// IsHealthy 返回缓存的健康状态
func (c *redisConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *redisConnector) Name() string {
	return c.cfg.Name
}

// GetClient 返回 Redis 客户端
func (c *redisConnector) GetClient() *redis.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client
}
