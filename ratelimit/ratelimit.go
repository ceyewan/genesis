// Package ratelimit 提供了限流组件，支持单机和分布式两种模式。
//
// ratelimit 是 Genesis 治理层的核心组件，它提供了：
// - 统一的 Limiter 接口，屏蔽单机和分布式差异
// - 单机模式：基于 golang.org/x/time/rate 的内存限流
// - 分布式模式：基于 Redis + Lua 的分布式限流
// - 令牌桶算法，支持突发流量
// - 开箱即用的 Gin 中间件
// - 与 L0 基础组件（日志、指标）的深度集成
//
// ## 基本使用
//
//	// 单机模式
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Driver: ratelimit.DriverStandalone,
//	    Standalone: &ratelimit.StandaloneConfig{
//	        CleanupInterval: 1 * time.Minute,
//	        IdleTimeout:     5 * time.Minute,
//	    },
//	}, ratelimit.WithLogger(logger))
//
//	// 检查是否允许请求
//	allowed, _ := limiter.Allow(ctx, "user:123", ratelimit.Limit{Rate: 10, Burst: 20})
//	if !allowed {
//	    return "rate limit exceeded"
//	}
//
// ## 分布式模式
//
//	redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
//	defer redisConn.Close()
//
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Driver: ratelimit.DriverDistributed,
//	    Distributed: &ratelimit.DistributedConfig{
//	        Prefix: "myapp:ratelimit:",
//	    },
//	}, ratelimit.WithRedisConnector(redisConn), ratelimit.WithLogger(logger))
//
//	allowed, _ := limiter.Allow(ctx, "api:/users", ratelimit.Limit{Rate: 100, Burst: 200})
//
// ## Gin 中间件
//
//	r := gin.New()
//	r.Use(ratelimit.GinMiddleware(limiter, &ratelimit.GinMiddlewareOptions{
//	    KeyFunc: func(c *gin.Context) string {
//	        return c.ClientIP()
//	    },
//	    LimitFunc: func(c *gin.Context) ratelimit.Limit {
//	        return ratelimit.Limit{Rate: 100, Burst: 200}
//	    },
//	}))
//
// ## 可观测性
//
// 通过注入 Logger 和 Meter 实现统一的日志和指标收集：
//
//	limiter, _ := ratelimit.New(cfg,
//	    ratelimit.WithLogger(logger),
//	    ratelimit.WithMeter(meter),
//	)
package ratelimit

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Limit 定义限流规则（令牌桶算法）
type Limit struct {
	Rate  float64 // 令牌生成速率（每秒生成多少个令牌）
	Burst int     // 令牌桶容量（突发最大请求数）
}

// Limiter 限流器核心接口
type Limiter interface {
	// Allow 尝试获取 1 个令牌（非阻塞）
	// key: 限流标识（如 IP, UserID, ServiceName）
	// limit: 限流规则
	// 返回: allowed（是否允许）, error（系统错误）
	//
	// 使用示例:
	//
	//	allowed, err := limiter.Allow(ctx, "user:123", ratelimit.Limit{Rate: 10, Burst: 20})
	//	if err != nil {
	//	    // 处理系统错误
	//	}
	//	if !allowed {
	//	    // 请求被限流
	//	}
	Allow(ctx context.Context, key string, limit Limit) (bool, error)

	// AllowN 尝试获取 N 个令牌（非阻塞）
	AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error)

	// Wait 阻塞等待直到获取 1 个令牌
	Wait(ctx context.Context, key string, limit Limit) error

	// Close 释放资源（如后台清理协程）
	Close() error
}

// ========================================
// 配置定义 (Configuration)
// ========================================

// DriverType 限流驱动类型
type DriverType string

const (
	// DriverStandalone 单机限流
	DriverStandalone DriverType = "standalone"
	// DriverDistributed 分布式限流
	DriverDistributed DriverType = "distributed"
)

// Config 限流组件统一配置
type Config struct {
	// Driver 限流模式: "standalone" | "distributed"
	Driver DriverType `json:"driver" yaml:"driver"`

	// Standalone 单机限流配置
	Standalone *StandaloneConfig `json:"standalone" yaml:"standalone"`

	// Distributed 分布式限流配置
	Distributed *DistributedConfig `json:"distributed" yaml:"distributed"`
}

// StandaloneConfig 单机限流配置
type StandaloneConfig struct {
	// CleanupInterval 清理过期限流器的间隔（默认：1 分钟）
	CleanupInterval time.Duration `json:"cleanup_interval" yaml:"cleanup_interval"`

	// IdleTimeout 限流器空闲超时时间（默认：5 分钟）
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`
}

// DistributedConfig 分布式限流配置
type DistributedConfig struct {
	// Prefix Redis Key 前缀（默认："ratelimit:"）
	Prefix string `json:"prefix" yaml:"prefix"`
}

func (c *Config) setDefaults() {
	if c == nil {
		return
	}
	switch c.Driver {
	case DriverStandalone:
		if c.Standalone == nil {
			c.Standalone = &StandaloneConfig{}
		}
		c.Standalone.setDefaults()
	case DriverDistributed:
		if c.Distributed == nil {
			c.Distributed = &DistributedConfig{}
		}
		c.Distributed.setDefaults()
	}
}

func (c *Config) validate() error {
	if c == nil {
		return ErrConfigNil
	}
	if c.Driver == "" {
		return xerrors.New("ratelimit: driver is required")
	}
	switch c.Driver {
	case DriverStandalone, DriverDistributed:
		return nil
	default:
		return xerrors.New("ratelimit: unsupported driver: " + string(c.Driver))
	}
}

func (c *StandaloneConfig) setDefaults() {
	if c == nil {
		return
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = 1 * time.Minute
	}
	if c.IdleTimeout <= 0 {
		c.IdleTimeout = 5 * time.Minute
	}
}

func (c *DistributedConfig) setDefaults() {
	if c == nil {
		return
	}
	if c.Prefix == "" {
		c.Prefix = "ratelimit:"
	}
}

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// Discard 返回一个静默的限流器实例（No-op 实现）
// 返回的 Limiter 实现了接口，但所有方法始终返回 true（允许通过），零开销
//
// 使用场景: 配置驱动的条件启用
//
//	var limiter ratelimit.Limiter
//	if cfg.RateLimitEnabled {
//	    limiter, _ = ratelimit.New(&ratelimit.Config{
//	        Driver: ratelimit.DriverStandalone,
//	        Standalone: &cfg.Standalone,
//	    }, ratelimit.WithLogger(logger))
//	} else {
//	    limiter = ratelimit.Discard()  // 零开销
//	}
func Discard() Limiter {
	return &noopLimiter{}
}

// noopLimiter 空限流器实现（非导出）
type noopLimiter struct{}

// Allow 始终返回 true（允许通过）
func (noop *noopLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
	return true, nil
}

// AllowN 始终返回 true（允许通过）
func (noop *noopLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error) {
	return true, nil
}

// Wait 始终返回 nil
func (noop *noopLimiter) Wait(ctx context.Context, key string, limit Limit) error {
	return nil
}

// Close 始终返回 nil
func (noop *noopLimiter) Close() error {
	return nil
}

// New 根据配置创建限流器
//
// 使用示例:
//
//	// 单机模式
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Driver: ratelimit.DriverStandalone,
//	    Standalone: &ratelimit.StandaloneConfig{
//	        CleanupInterval: 1 * time.Minute,
//	    },
//	}, ratelimit.WithLogger(logger))
//
//	// 分布式模式（需注入 Redis 连接器）
//	redisConn, _ := connector.NewRedis(&cfg.Redis)
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Driver: ratelimit.DriverDistributed,
//	    Distributed: &ratelimit.DistributedConfig{Prefix: "myapp:"},
//	}, ratelimit.WithRedisConnector(redisConn), ratelimit.WithLogger(logger))
func New(cfg *Config, opts ...Option) (Limiter, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// 应用选项（需要先提取 WithRedisConnector）
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	if o.logger == nil {
		o.logger = clog.Discard()
	}
	if o.meter == nil {
		o.meter = metrics.Discard()
	}

	logger := o.logger.With(clog.String("component", "ratelimit"))

	switch cfg.Driver {
	case DriverStandalone:
		return newStandalone(cfg.Standalone, logger, o.meter)
	case DriverDistributed:
		// 使用 Option 中注入的 redisConn
		if o.redisConn == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required_for_distributed_mode")
		}
		return newDistributed(cfg.Distributed, o.redisConn, logger, o.meter)
	default:
		return nil, xerrors.New("ratelimit: unsupported driver: " + string(cfg.Driver))
	}
}
