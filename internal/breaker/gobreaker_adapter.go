package breaker

import (
	"context"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/breaker/types"
	"github.com/ceyewan/genesis/pkg/clog"
	metrics "github.com/ceyewan/genesis/pkg/metrics"
	"github.com/sony/gobreaker/v2"
)

// GoBreakerAdapter 适配 gobreaker 库到我们的接口
type GoBreakerAdapter struct {
	cfg      types.Config
	breakers sync.Map // key: service name, value: *gobreaker.CircuitBreaker[any]
	logger   clog.Logger
	meter    metrics.Meter
}

// NewGoBreakerAdapter 创建基于 gobreaker 的适配器
func NewGoBreakerAdapter(cfg *types.Config, logger clog.Logger, meter metrics.Meter) (*GoBreakerAdapter, error) {
	if cfg == nil {
		cfg = types.DefaultConfig()
	}

	adapter := &GoBreakerAdapter{
		cfg:    *cfg,
		logger: logger,
		meter:  meter,
	}

	if logger != nil {
		logger.Info("gobreaker adapter initialized")
	}

	return adapter, nil
}

// Execute 执行受保护的函数
func (a *GoBreakerAdapter) Execute(ctx context.Context, key string, fn func() error) error {
	cb := a.getCircuitBreaker(key)

	_, err := cb.Execute(func() (any, error) {
		return nil, fn()
	})

	return err
}

// ExecuteWithFallback 执行受保护的函数，并提供降级逻辑
func (a *GoBreakerAdapter) ExecuteWithFallback(ctx context.Context, key string, fn func() error, fallback func(error) error) error {
	cb := a.getCircuitBreaker(key)

	_, err := cb.Execute(func() (any, error) {
		return nil, fn()
	})

	// 如果是熔断错误且提供了降级函数，执行降级
	if err != nil && fallback != nil {
		if isCircuitBreakerError(err) {
			return fallback(err)
		}
	}

	return err
}

// State 获取指定服务的熔断器状态
func (a *GoBreakerAdapter) State(key string) types.State {
	cb := a.getCircuitBreaker(key)

	switch cb.State() {
	case gobreaker.StateClosed:
		return types.StateClosed
	case gobreaker.StateOpen:
		return types.StateOpen
	case gobreaker.StateHalfOpen:
		return types.StateHalfOpen
	default:
		return types.StateClosed
	}
}

// Reset 手动重置指定服务的熔断器状态为 Closed
func (a *GoBreakerAdapter) Reset(key string) {
	// gobreaker 没有直接的 Reset 方法，我们需要重新创建实例
	a.breakers.Delete(key)

	if a.logger != nil {
		a.logger.Info("circuit breaker manually reset",
			clog.String("service", key))
	}
}

// getCircuitBreaker 获取或创建熔断器实例
func (a *GoBreakerAdapter) getCircuitBreaker(key string) *gobreaker.CircuitBreaker[any] {
	// 从缓存中获取
	if val, ok := a.breakers.Load(key); ok {
		return val.(*gobreaker.CircuitBreaker[any])
	}

	// 创建新的熔断器实例
	settings := a.createSettings(key)
	cb := gobreaker.NewCircuitBreaker[any](settings)

	// 存储到缓存中
	actual, _ := a.breakers.LoadOrStore(key, cb)
	return actual.(*gobreaker.CircuitBreaker[any])
}

// createSettings 根据配置创建 gobreaker Settings
func (a *GoBreakerAdapter) createSettings(serviceName string) gobreaker.Settings {
	// 获取服务特定策略或默认策略
	policy := a.cfg.Default
	if svcPolicy, ok := a.cfg.Services[serviceName]; ok {
		policy = mergePolicy(policy, svcPolicy)
	}

	return gobreaker.Settings{
		Name:        serviceName,
		MaxRequests: uint32(policy.HalfOpenMaxRequests),
		Interval:    0, // 使用 BucketPeriod 而不是 Interval
		Timeout:     policy.OpenTimeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// 计算失败率
			totalRequests := counts.Requests
			if totalRequests < uint32(policy.MinRequests) {
				return false
			}

			failureRate := float64(counts.TotalFailures) / float64(totalRequests)
			return failureRate >= policy.FailureThreshold
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			if a.logger != nil {
				a.logger.Info("circuit breaker state changed",
					clog.String("service", name),
					clog.String("from", from.String()),
					clog.String("to", to.String()))
			}
		},
		IsSuccessful: func(err error) bool {
			// 根据 CountTimeout 配置决定是否将超时视为成功
			if err == nil {
				return true
			}
			// 这里可以根据需要添加更复杂的逻辑
			// 比如区分业务错误和系统错误
			return false
		},
		BucketPeriod: time.Duration(policy.WindowSize) * time.Millisecond, // 简化处理
	}
}

// isCircuitBreakerError 判断错误是否为熔断器错误
func isCircuitBreakerError(err error) bool {
	if err == nil {
		return false
	}
	// gobreaker 在熔断状态下返回特定的错误
	return err.Error() == "circuit breaker is open" || err.Error() == "too many requests in half-open state"
}
