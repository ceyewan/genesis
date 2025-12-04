// pkg/connector/redis.go
package connector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/metrics"
	"github.com/ceyewan/genesis/pkg/xerrors"
	"github.com/redis/go-redis/v9"
)

type redisConnector struct {
	cfg                   *RedisConfig
	client                *redis.Client
	logger                clog.Logger
	meter                 metrics.Meter
	healthy               atomic.Bool
	mu                    sync.RWMutex
	totalConnections      metrics.Counter
	successfulConnections metrics.Counter
	failedConnections     metrics.Counter
	activeConnections     metrics.Gauge
}

// NewRedis 创建 Redis 连接器
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
	if err := cfg.Validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid redis config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}

	c := &redisConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "redis"), clog.String("name", cfg.Name)),
		meter:  opt.meter,
	}

	// 创建简化指标
	if c.meter != nil {
		var err error
		c.totalConnections, err = c.meter.Counter(
			"connector_redis_total_connections",
			"Total number of Redis connection attempts",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create total connections counter")
		}

		c.successfulConnections, err = c.meter.Counter(
			"connector_redis_successful_connections",
			"Number of successful Redis connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create successful connections counter")
		}

		c.failedConnections, err = c.meter.Counter(
			"connector_redis_failed_connections",
			"Number of failed Redis connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create failed connections counter")
		}

		c.activeConnections, err = c.meter.Gauge(
			"connector_redis_active_connections",
			"Number of active Redis connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create active connections gauge")
		}
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
	})

	return c, nil
}

// Connect 建立连接
func (c *redisConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 记录总连接尝试
	if c.totalConnections != nil {
		c.totalConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
	}

	c.logger.Info("attempting to connect to redis", clog.String("addr", c.cfg.Addr))

	if err := c.client.Ping(ctx).Err(); err != nil {
		// 记录失败连接
		if c.failedConnections != nil {
			c.failedConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
		}
		c.logger.Error("failed to connect to redis", clog.Error(err), clog.String("addr", c.cfg.Addr))
		return NewError(c.cfg.Name, TypeConnection, err, true)
	}

	// 记录成功连接
	if c.successfulConnections != nil {
		c.successfulConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
	}
	if c.activeConnections != nil {
		c.activeConnections.Set(ctx, float64(1), metrics.L("connector", c.cfg.Name))
	}

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

	// 减少活跃连接数
	if c.activeConnections != nil {
		c.activeConnections.Set(context.Background(), float64(0), metrics.L("connector", c.cfg.Name))
	}

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
		return WrapError(c.cfg.Name, err, true)
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

// Validate 实现 Configurable 接口
func (c *redisConnector) Validate() error {
	return c.cfg.Validate()
}

// MustNewRedis 创建 Redis 连接器，失败时 panic
func MustNewRedis(cfg *RedisConfig, opts ...Option) RedisConnector {
	conn, err := NewRedis(cfg, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create redis connector: %v", err))
	}
	return conn
}
