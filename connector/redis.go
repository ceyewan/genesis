package connector

import (
	"context"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
)

type redisConnector struct {
	cfg     *RedisConfig
	client  *redis.Client
	logger  clog.Logger
	healthy atomic.Bool
}

// NewRedis 创建 Redis 连接器
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
	cfg.setDefaults()
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

	// 创建 Redis 客户端
	c.client = redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaintNotificationsConfig: &maintnotifications.Config{
			Mode: maintnotifications.ModeDisabled,
		},
	})

	return c, nil
}

// Connect 建立连接
func (c *redisConnector) Connect(ctx context.Context) error {
	c.logger.Info("attempting to connect to redis", clog.String("addr", c.cfg.Addr))

	if err := c.client.Ping(ctx).Err(); err != nil {
		c.logger.Error("failed to connect to redis", clog.Error(err), clog.String("addr", c.cfg.Addr))
		return xerrors.Wrapf(err, "redis connector[%s]: connection failed", c.cfg.Name)
	}

	c.healthy.Store(true)
	c.logger.Info("successfully connected to redis", clog.String("addr", c.cfg.Addr))

	return nil
}

// Close 关闭连接
func (c *redisConnector) Close() error {
	c.logger.Info("closing redis connection", clog.String("addr", c.cfg.Addr))
	c.healthy.Store(false)

	if c.client != nil {
		err := c.client.Close()
		if err != nil {
			c.logger.Error("failed to close redis connection", clog.Error(err))
			return err
		}
		c.logger.Info("redis connection closed successfully")
	}
	return nil
}

// HealthCheck 检查连接健康状态
func (c *redisConnector) HealthCheck(ctx context.Context) error {
	if err := c.client.Ping(ctx).Err(); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("redis health check failed", clog.Error(err))
		return xerrors.Wrapf(err, "redis connector[%s]: health check failed", c.cfg.Name)
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
	return c.client
}
