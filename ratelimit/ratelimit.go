// Package ratelimit 提供 Genesis 的限流组件。
//
// `ratelimit` 位于治理层（L3），面向两类常见需求：
// 1. 进程内的轻量限流；
// 2. 基于 Redis 的集群共享限流。
//
// 这个包的核心能力是非阻塞的 `Allow` / `AllowN` 检查。单机模式使用
// `golang.org/x/time/rate`，分布式模式使用 Redis Lua 脚本维护共享桶状态。
//
// 分布式模式有几个重要语义：
// - 桶状态按 `key + limit` 隔离，不同 `Rate/Burst` 不会共享同一个 Redis 键。
// - 脚本使用 Redis `TIME` 作为统一时钟，避免多节点本地时钟漂移破坏限流精度。
// - `Wait` 不是分布式能力，调用会返回 `ErrNotSupported`。
//
// Gin 中间件和 gRPC 拦截器默认采用 `fail_open`，即限流器内部异常时放行业务请求；
// 如果希望把限流器异常视为保护失败，可切换到 `fail_closed`。
//
// 基本用法：
//
//	limiter, _ := ratelimit.New(&ratelimit.Config{
//	    Driver: ratelimit.DriverStandalone,
//	}, ratelimit.WithLogger(logger))
//
//	allowed, err := limiter.Allow(ctx, "user:123", ratelimit.Limit{
//	    Rate:  10,
//	    Burst: 20,
//	})
//
// 分布式用法：
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

// ErrorPolicy 定义限流检查出错时的处理策略。
type ErrorPolicy string

const (
	// ErrorPolicyFailOpen 表示限流器出错时放行请求。
	ErrorPolicyFailOpen ErrorPolicy = "fail_open"
	// ErrorPolicyFailClosed 表示限流器出错时拒绝请求。
	ErrorPolicyFailClosed ErrorPolicy = "fail_closed"
)

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
