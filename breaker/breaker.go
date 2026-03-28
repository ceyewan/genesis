// Package breaker 提供了面向 gRPC 客户端场景的轻量熔断组件。
//
// breaker 在 Genesis 治理层中承担“故障隔离”职责：当下游服务出现系统性错误时，
// 组件会按 key 维度独立统计失败并驱动 closed/open/half-open 状态迁移，避免故障
// 扩散到整个调用链。
//
// 当前组件的定位比较克制：
//   - 核心能力是 Execute、State 和 gRPC UnaryClientInterceptor
//   - 默认以 cc.Target() 作为服务级熔断 key，也支持通过 WithKeyFunc 自定义粒度
//   - gRPC 拦截器会区分系统性错误与业务错误，避免把 InvalidArgument、NotFound
//     等明显业务错误直接计入熔断统计
//   - WithFallback 只负责处理“请求被 breaker 拒绝”这一类情况，不负责生成替代结果
//
// breaker 不试图替业务统一建模所有失败语义。对于不同调用场景，最重要的设计点
// 不是状态机本身，而是 key 粒度和失败口径。
package breaker

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"google.golang.org/grpc"
)

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Breaker 熔断器核心接口
type Breaker interface {
	// Execute 执行受熔断保护的函数
	// key: 熔断键（可以是服务名、后端地址、方法名等）
	// fn: 要执行的函数
	// 返回: 函数执行结果和错误。
	// 若执行被熔断器拒绝，返回组件自己的拒绝错误；若配置了 Fallback，
	// Fallback 成功时返回 nil, nil。
	Execute(ctx context.Context, key string, fn func() (any, error)) (any, error)

	// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
	// 支持 InterceptorOption 配置 Key 生成策略
	UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor

	// State 获取指定键的熔断器状态
	State(key string) (State, error)
}

// State 熔断器状态
type State int

const (
	// StateClosed 闭合状态（正常）
	StateClosed State = iota
	// StateHalfOpen 半开状态（探测恢复）
	StateHalfOpen
	// StateOpen 打开状态（熔断中）
	StateOpen
)

// String 返回状态的字符串表示
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half_open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// ========================================
// 配置定义 (Configuration)
// ========================================

// Config 熔断器配置
type Config struct {
	// MaxRequests 半开状态下允许通过的最大请求数（默认：1）
	// 用于探测服务是否恢复
	MaxRequests uint32 `json:"max_requests" yaml:"max_requests"`

	// Interval 闭合状态下的统计周期（默认：0，不清空统计）
	// 设置后会按周期重置闭合状态下的统计计数
	Interval time.Duration `json:"interval" yaml:"interval"`

	// Timeout 打开状态持续时间（默认：60s）
	// 超时后进入半开状态进行探测
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// FailureRatio 失败率阈值（默认：0.6，即 60%）
	// 当失败率超过此值时触发熔断
	FailureRatio float64 `json:"failure_ratio" yaml:"failure_ratio"`

	// MinimumRequests 触发熔断的最小请求数（默认：10）
	// 请求数少于此值时不会触发熔断
	MinimumRequests uint32 `json:"minimum_requests" yaml:"minimum_requests"`
}

// validate 验证配置并设置默认值（内部使用）。
func (c *Config) validate() error {
	if c.MaxRequests == 0 {
		c.MaxRequests = 1
	}
	if c.Interval < 0 {
		return xerrors.Wrap(ErrInvalidConfig, "interval must be greater than or equal to 0")
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if c.Timeout < 0 {
		return xerrors.Wrap(ErrInvalidConfig, "timeout must be greater than 0")
	}
	if c.FailureRatio == 0 {
		c.FailureRatio = 0.6
	}
	if c.FailureRatio <= 0 || c.FailureRatio > 1 {
		return xerrors.Wrap(ErrInvalidConfig, "failure_ratio must be within (0, 1]")
	}
	if c.MinimumRequests == 0 {
		c.MinimumRequests = 10
	}
	return nil
}

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// New 创建熔断器实例
// 这是标准的工厂函数，支持在不依赖其他容器的情况下独立实例化
//
// 参数:
//   - cfg: 熔断器配置，传 nil 时使用默认配置
//   - opts: 可选参数 (Logger, Fallback)
//
// 返回: Breaker 实例和错误。
func New(cfg *Config, opts ...Option) (Breaker, error) {
	// nil cfg 时使用默认配置
	if cfg == nil {
		cfg = &Config{}
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// nil logger 时使用 Discard（确保 logger 永远不为 nil）
	logger := opt.logger
	if logger == nil {
		logger = clog.Discard()
	}

	logger.Info("creating circuit breaker",
		clog.Int("max_requests", int(cfg.MaxRequests)),
		clog.Duration("interval", cfg.Interval),
		clog.Duration("timeout", cfg.Timeout),
		clog.Float64("failure_ratio", cfg.FailureRatio),
		clog.Int("minimum_requests", int(cfg.MinimumRequests)))

	return newBreaker(cfg, logger, opt.fallback)
}
