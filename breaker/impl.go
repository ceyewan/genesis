package breaker

import (
	"context"
	"sync"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/sony/gobreaker/v2"
)

// circuitBreaker 熔断器实现（非导出）
type circuitBreaker struct {
	cfg               *Config
	logger            clog.Logger
	fallback          FallbackFunc
	failureClassifier FailureClassifier
	executions        metrics.Counter
	rejections        metrics.Counter
	stateGauge        metrics.Gauge

	// 服务级熔断器管理
	mu       sync.RWMutex
	breakers map[string]*gobreaker.CircuitBreaker[any]
}

// newBreaker 创建熔断器实例（内部函数）
// 注意：cfg 已在 New() 中调用 validate() 设置了默认值，logger 已在 WithLogger() 中处理
func newBreaker(
	cfg *Config,
	logger clog.Logger,
	meter metrics.Meter,
	fallback FallbackFunc,
	failureClassifier FailureClassifier,
) (Breaker, error) {
	if meter == nil {
		meter = metrics.Discard()
	}
	executions := breakerCounter(meter, MetricExecutions, "Number of breaker executions")
	rejections := breakerCounter(meter, MetricRejections, "Number of breaker rejections")
	stateGauge := breakerGauge(meter, MetricState, "Current breaker state: closed=0, half_open=1, open=2")
	cb := &circuitBreaker{
		cfg:               cfg,
		logger:            logger,
		fallback:          fallback,
		failureClassifier: failureClassifier,
		executions:        executions,
		rejections:        rejections,
		stateGauge:        stateGauge,
		breakers:          make(map[string]*gobreaker.CircuitBreaker[any]),
	}

	logger.Info("circuit breaker created",
		clog.Int("max_requests", int(cfg.MaxRequests)),
		clog.Duration("timeout", cfg.Timeout),
		clog.Float64("failure_ratio", cfg.FailureRatio),
		clog.Int("minimum_requests", int(cfg.MinimumRequests)))

	return cb, nil
}

func breakerCounter(meter metrics.Meter, name, description string) metrics.Counter {
	counter, err := meter.Counter(name, description)
	if err == nil && counter != nil {
		return counter
	}
	counter, _ = metrics.Discard().Counter(name, description)
	return counter
}

func breakerGauge(meter metrics.Meter, name, description string) metrics.Gauge {
	gauge, err := meter.Gauge(name, description)
	if err == nil && gauge != nil {
		return gauge
	}
	gauge, _ = metrics.Discard().Gauge(name, description)
	return gauge
}

// Execute 执行受熔断保护的函数
func (cb *circuitBreaker) Execute(ctx context.Context, key string, fn func() (any, error)) (any, error) {
	if key == "" {
		return nil, ErrKeyEmpty
	}

	// 获取或创建熔断器
	breaker, err := cb.getOrCreateBreaker(key)
	if err != nil {
		return nil, err
	}

	// 执行熔断保护的函数
	result, err := breaker.Execute(fn)

	rejectionErr, rejected := mapBreakerError(err)
	if rejected {
		cb.rejections.Inc(ctx, metrics.L("key", key), metrics.L("reason", rejectionErr.Error()))
		cb.logger.Info("Circuit breaker rejected request",
			clog.String("key", key),
			clog.Error(rejectionErr))

		if cb.fallback != nil {
			return cb.fallback(ctx, key, rejectionErr)
		}

		return nil, rejectionErr
	}

	resultLabel := "success"
	if err != nil {
		resultLabel = "failure"
	}
	cb.executions.Inc(ctx, metrics.L("key", key), metrics.L("result", resultLabel))
	return result, err
}

// State 获取指定键的熔断器状态
func (cb *circuitBreaker) State(key string) (State, error) {
	if key == "" {
		return StateClosed, ErrKeyEmpty
	}

	cb.mu.RLock()
	breaker, ok := cb.breakers[key]
	cb.mu.RUnlock()
	if !ok {
		return StateClosed, nil
	}

	state := breaker.State()

	switch state {
	case gobreaker.StateClosed:
		return StateClosed, nil
	case gobreaker.StateHalfOpen:
		return StateHalfOpen, nil
	case gobreaker.StateOpen:
		return StateOpen, nil
	default:
		return StateClosed, nil
	}
}

// getOrCreateBreaker 获取或创建指定键的熔断器
func (cb *circuitBreaker) getOrCreateBreaker(key string) (*gobreaker.CircuitBreaker[any], error) {
	cb.mu.RLock()
	breaker, ok := cb.breakers[key]
	cb.mu.RUnlock()
	if ok {
		return breaker, nil
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if breaker, ok = cb.breakers[key]; ok {
		return breaker, nil
	}
	if len(cb.breakers) >= cb.cfg.MaxKeys {
		return nil, ErrKeyLimitExceeded
	}

	// 创建新的熔断器
	settings := gobreaker.Settings{
		Name:        key,
		MaxRequests: cb.cfg.MaxRequests,
		Interval:    cb.cfg.Interval,
		Timeout:     cb.cfg.Timeout,
		ReadyToTrip: cb.readyToTrip,
		IsSuccessful: func(err error) bool {
			if cb.failureClassifier == nil {
				return err == nil
			}
			return err == nil || !cb.failureClassifier(err)
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			cb.onStateChange(name, from, to)
		},
	}

	breaker = gobreaker.NewCircuitBreaker[any](settings)
	cb.breakers[key] = breaker
	return breaker, nil
}

// readyToTrip 判断是否应该触发熔断
func (cb *circuitBreaker) readyToTrip(counts gobreaker.Counts) bool {
	// 请求数少于最小请求数，不触发熔断
	if counts.Requests < cb.cfg.MinimumRequests {
		return false
	}

	// 计算失败率
	failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)

	// 失败率超过阈值，触发熔断
	return failureRatio >= cb.cfg.FailureRatio
}

// onStateChange 状态变更回调
func (cb *circuitBreaker) onStateChange(name string, from, to gobreaker.State) {
	cb.stateGauge.Set(context.Background(), float64(to), metrics.L("key", name))
	if cb.logger != nil {
		cb.logger.Info("circuit breaker state changed",
			clog.String("service", name),
			clog.String("from", stateToString(from)),
			clog.String("to", stateToString(to)))
	}
}

// stateToString 将 gobreaker.State 转换为字符串
func stateToString(state gobreaker.State) string {
	switch state {
	case gobreaker.StateClosed:
		return "closed"
	case gobreaker.StateHalfOpen:
		return "half_open"
	case gobreaker.StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

func mapBreakerError(err error) (error, bool) {
	switch {
	case err == nil:
		return nil, false
	case xerrors.Is(err, gobreaker.ErrOpenState):
		return ErrOpenState, true
	case xerrors.Is(err, gobreaker.ErrTooManyRequests):
		return ErrTooManyRequests, true
	default:
		return nil, false
	}
}
