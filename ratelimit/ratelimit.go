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
//	limiter, _ := ratelimit.NewStandalone(&ratelimit.StandaloneConfig{
//	    CleanupInterval: 1 * time.Minute,
//	    IdleTimeout:     5 * time.Minute,
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
//	limiter, _ := ratelimit.NewDistributed(&ratelimit.DistributedConfig{
//	    Prefix: "myapp:ratelimit:",
//	}, redisConn, ratelimit.WithLogger(logger))
//
//	allowed, _ := limiter.Allow(ctx, "api:/users", ratelimit.Limit{Rate: 100, Burst: 200})
//
// ## Gin 中间件
//
//	r := gin.New()
//	r.Use(ratelimit.GinMiddleware(limiter, nil, func(c *gin.Context) ratelimit.Limit {
//	    return ratelimit.Limit{Rate: 100, Burst: 200}
//	}))
//
// ## 可观测性
//
// 通过注入 Logger 和 Meter 实现统一的日志和指标收集：
//
//	limiter, _ := ratelimit.NewStandalone(cfg,
//	    ratelimit.WithLogger(logger),
//	    ratelimit.WithMeter(meter),
//	)
package ratelimit

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
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
}

// ========================================
// 配置定义 (Configuration)
// ========================================

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

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// NewStandalone 创建单机限流器
// 这是标准的工厂函数，支持在不依赖其他容器的情况下独立实例化
//
// 参数:
//   - cfg: 单机限流配置
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	limiter, _ := ratelimit.NewStandalone(&ratelimit.StandaloneConfig{
//	    CleanupInterval: 1 * time.Minute,
//	    IdleTimeout:     5 * time.Minute,
//	}, ratelimit.WithLogger(logger))
func NewStandalone(cfg *StandaloneConfig, opts ...Option) (Limiter, error) {
	if cfg == nil {
		cfg = &StandaloneConfig{
			CleanupInterval: 1 * time.Minute,
			IdleTimeout:     5 * time.Minute,
		}
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger（添加 component 字段）
	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "ratelimit"))
	}

	if logger != nil {
		logger.Info("creating standalone rate limiter")
	}

	return newStandalone(cfg, logger, opt.meter)
}

// NewDistributed 创建分布式限流器
// 这是标准的工厂函数，支持在不依赖其他容器的情况下独立实例化
//
// 参数:
//   - redisConn: Redis 连接器
//   - cfg: 分布式限流配置
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	redisConn, _ := connector.NewRedis(redisConfig)
//	limiter, _ := ratelimit.NewDistributed(redisConn, &ratelimit.DistributedConfig{
//	    Prefix: "myapp:ratelimit:",
//	}, ratelimit.WithLogger(logger))
func NewDistributed(redisConn connector.RedisConnector, cfg *DistributedConfig, opts ...Option) (Limiter, error) {
	if redisConn == nil {
		return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
	}

	if cfg == nil {
		cfg = &DistributedConfig{
			Prefix: "ratelimit:",
		}
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger（添加 component 字段）
	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "ratelimit"))
	}

	if logger != nil {
		logger.Info("creating distributed rate limiter")
	}

	return newDistributed(cfg, redisConn, logger, opt.meter)
}
