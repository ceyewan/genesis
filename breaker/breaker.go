// Package breaker 提供了熔断器组件，专注于 gRPC 客户端的故障隔离与自动恢复。
//
// breaker 是 Genesis 治理层的核心组件，它提供了：
// - 基于 gobreaker 的熔断器实现
// - 服务级粒度的熔断管理（按目标服务名独立熔断）
// - 自动故障隔离和自动恢复（通过半开状态探测）
// - 灵活的降级策略（快速失败或自定义降级逻辑）
// - gRPC Unary Interceptor 无侵入集成
//
// ## 基本使用
//
//	// 创建熔断器
//	brk, _ := breaker.New(&breaker.Config{
//		MaxRequests:         5,
//		Interval:            60 * time.Second,
//		Timeout:             30 * time.Second,
//		FailureRatio:        0.6,
//		MinimumRequests:     10,
//	}, breaker.WithLogger(logger))
//
//	// 使用 gRPC Interceptor
//	conn, _ := grpc.NewClient(
//		"localhost:9001",
//		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
//	)
//
// ## 降级策略
//
//	// 自定义降级逻辑
//	brk, _ := breaker.New(cfg,
//		breaker.WithLogger(logger),
//		breaker.WithFallback(func(ctx context.Context, serviceName string, err error) error {
//			// 返回缓存数据或默认值
//			return nil
//		}),
//	)
package breaker

import (
	"context"
	"time"

	"github.com/ceyewan/genesis/clog"

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
	// 返回: 函数执行结果和错误
	Execute(ctx context.Context, key string, fn func() (interface{}, error)) (interface{}, error)

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
	// 设置后会定期清空计数器
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

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// New 创建熔断器实例
// 这是标准的工厂函数，支持在不依赖其他容器的情况下独立实例化
//
// 参数:
//   - cfg: 熔断器配置
//   - opts: 可选参数 (Logger, Fallback)
//
// 使用示例:
//
//	brk, _ := breaker.New(&breaker.Config{
//		MaxRequests:     5,
//		Interval:        60 * time.Second,
//		Timeout:         30 * time.Second,
//		FailureRatio:    0.6,
//		MinimumRequests: 10,
//	}, breaker.WithLogger(logger))
func New(cfg *Config, opts ...Option) (Breaker, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger（添加 component 字段）
	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "breaker"))
	}

	if logger != nil {
		logger.Info("creating circuit breaker",
			clog.Int("max_requests", int(cfg.MaxRequests)),
			clog.Duration("interval", cfg.Interval),
			clog.Duration("timeout", cfg.Timeout),
			clog.Float64("failure_ratio", cfg.FailureRatio),
			clog.Int("minimum_requests", int(cfg.MinimumRequests)))
	}

	return newBreaker(cfg, logger, opt.fallback)
}
