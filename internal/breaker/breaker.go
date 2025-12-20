package breaker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ceyewan/genesis/pkg/breaker/types"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/metrics"
)

// Manager 熔断器管理器
type Manager struct {
	cfg      types.Config
	breakers sync.Map // key: service name, value: *circuitBreaker
	logger   clog.Logger
	meter    metrics.Meter
	tracer   interface{} // TODO: 实现 Tracer 接口
}

// circuitBreaker 单个服务的熔断器实例
type circuitBreaker struct {
	policy   types.Policy
	state    types.State
	window   *Window
	openTime time.Time    // Open 状态开始时间
	halfOpen atomic.Int32 // HalfOpen 状态下的请求计数器
	mu       sync.RWMutex
}

// New 创建熔断器管理器（使用 gobreaker 库）
func New(cfg *types.Config, logger clog.Logger, meter metrics.Meter) (types.Breaker, error) {
	return NewGoBreakerAdapter(cfg, logger, meter)
}

// Execute 执行受保护的函数
func (m *Manager) Execute(ctx context.Context, key string, fn func() error) error {
	cb := m.getCircuitBreaker(key)

	// 检查状态
	if err := cb.allowRequest(); err != nil {
		return err
	}

	// 执行函数
	err := fn()

	// 记录结果
	cb.recordResult(err == nil)

	return err
}

// ExecuteWithFallback 执行受保护的函数，并提供降级逻辑
func (m *Manager) ExecuteWithFallback(ctx context.Context, key string, fn func() error, fallback func(error) error) error {
	cb := m.getCircuitBreaker(key)

	// 检查状态
	if err := cb.allowRequest(); err != nil {
		// 熔断状态，执行降级逻辑
		if fallback != nil {
			return fallback(err)
		}
		return err
	}

	// 执行函数
	err := fn()

	// 记录结果
	cb.recordResult(err == nil)

	return err
}

// State 获取指定服务的熔断器状态
func (m *Manager) State(key string) types.State {
	cb := m.getCircuitBreaker(key)
	return cb.getState()
}

// Reset 手动重置指定服务的熔断器状态为 Closed
func (m *Manager) Reset(key string) {
	cb := m.getCircuitBreaker(key)
	cb.reset()

	if m.logger != nil {
		m.logger.Info("circuit breaker manually reset",
			clog.String("service", key))
	}
}

// getCircuitBreaker 获取或创建熔断器实例
func (m *Manager) getCircuitBreaker(key string) *circuitBreaker {
	// 从缓存中获取
	if val, ok := m.breakers.Load(key); ok {
		return val.(*circuitBreaker)
	}

	// 创建新的熔断器实例
	policy := m.cfg.Default
	if svcPolicy, ok := m.cfg.Services[key]; ok {
		// 合并服务特定策略与默认策略
		policy = mergePolicy(policy, svcPolicy)
	}

	cb := &circuitBreaker{
		policy: policy,
		state:  types.StateClosed,
		window: NewWindow(policy.WindowSize),
	}

	// 存储到缓存中
	actual, _ := m.breakers.LoadOrStore(key, cb)
	return actual.(*circuitBreaker)
}

// allowRequest 检查是否允许请求通过
func (cb *circuitBreaker) allowRequest() error {
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	switch state {
	case types.StateClosed:
		return nil // 正常通过

	case types.StateOpen:
		// 检查是否到了半开探测时间
		if time.Since(cb.openTime) >= cb.policy.OpenTimeout {
			cb.transitionTo(types.StateHalfOpen)
			cb.halfOpen.Store(0)
			return nil // 允许探测请求
		}
		return types.ErrOpenState

	case types.StateHalfOpen:
		// 检查探测请求数是否已达上限
		count := cb.halfOpen.Add(1)
		if count > int32(cb.policy.HalfOpenMaxRequests) {
			cb.halfOpen.Add(-1) // 回滚计数
			return types.ErrTooManyRequests
		}
		return nil // 允许探测请求

	default:
		return fmt.Errorf("unknown circuit breaker state: %s", state)
	}
}

// recordResult 记录请求结果
func (cb *circuitBreaker) recordResult(success bool) {
	cb.window.Record(success)

	// 根据当前状态和结果决定状态转换
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case types.StateClosed:
		// 检查是否需要熔断
		if cb.shouldOpen() {
			cb.transitionTo(types.StateOpen)
			cb.openTime = time.Now()
		}

	case types.StateHalfOpen:
		if success {
			// 探测成功，关闭熔断器
			cb.transitionTo(types.StateClosed)
			cb.window.Reset()
			cb.halfOpen.Store(0)
		} else {
			// 探测失败，重新打开熔断器
			cb.transitionTo(types.StateOpen)
			cb.openTime = time.Now()
			cb.halfOpen.Store(0)
		}
	}
}

// shouldOpen 判断是否应该打开熔断器
func (cb *circuitBreaker) shouldOpen() bool {
	total := cb.window.Total()
	if total < cb.policy.MinRequests {
		return false // 请求数不足，不触发熔断
	}

	failureRate := cb.window.FailureRate()
	return failureRate >= cb.policy.FailureThreshold
}

// getState 获取当前状态
func (cb *circuitBreaker) getState() types.State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// transitionTo 状态转换
func (cb *circuitBreaker) transitionTo(newState types.State) {
	oldState := cb.state
	cb.state = newState

	// 记录日志
	if cb.policy.FailureThreshold > 0 { // 确保有有效的策略
		// 这里可以添加日志记录
		_ = oldState // 避免未使用变量警告
	}
}

// mergePolicy 合并策略，服务特定策略覆盖默认策略
func mergePolicy(defaultPolicy, servicePolicy types.Policy) types.Policy {
	// 复制默认策略
	result := defaultPolicy

	// 用服务特定策略覆盖
	if servicePolicy.FailureThreshold > 0 {
		result.FailureThreshold = servicePolicy.FailureThreshold
	}
	if servicePolicy.WindowSize > 0 {
		result.WindowSize = servicePolicy.WindowSize
	}
	if servicePolicy.MinRequests > 0 {
		result.MinRequests = servicePolicy.MinRequests
	}
	if servicePolicy.OpenTimeout > 0 {
		result.OpenTimeout = servicePolicy.OpenTimeout
	}
	if servicePolicy.HalfOpenMaxRequests > 0 {
		result.HalfOpenMaxRequests = servicePolicy.HalfOpenMaxRequests
	}
	// CountTimeout 是布尔值，总是使用服务特定策略的值

	return result
}

// reset 重置熔断器
func (cb *circuitBreaker) reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = types.StateClosed
	cb.window.Reset()
	cb.halfOpen.Store(0)
}
