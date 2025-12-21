package breaker

import (
	"context"
	"sync"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/sony/gobreaker/v2"
)

// circuitBreaker 熔断器实现（非导出）
type circuitBreaker struct {
	cfg      *Config
	logger   clog.Logger
	meter    metrics.Meter
	fallback FallbackFunc

	// 服务级熔断器管理
	breakers sync.Map // map[string]*gobreaker.CircuitBreaker[interface{}]
}

// newBreaker 创建熔断器实例（内部函数）
func newBreaker(
	cfg *Config,
	logger clog.Logger,
	meter metrics.Meter,
	fallback FallbackFunc,
) (Breaker, error) {
	// 设置默认值
	if cfg.MaxRequests == 0 {
		cfg.MaxRequests = 1
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.FailureRatio == 0 {
		cfg.FailureRatio = 0.6
	}
	if cfg.MinimumRequests == 0 {
		cfg.MinimumRequests = 10
	}

	cb := &circuitBreaker{
		cfg:      cfg,
		logger:   logger,
		meter:    meter,
		fallback: fallback,
	}

	if logger != nil {
		logger.Info("circuit breaker created",
			clog.Int("max_requests", int(cfg.MaxRequests)),
			clog.Duration("timeout", cfg.Timeout),
			clog.Float64("failure_ratio", cfg.FailureRatio),
			clog.Int("minimum_requests", int(cfg.MinimumRequests)))
	}

	return cb, nil
}

// Execute 执行受熔断保护的函数
func (cb *circuitBreaker) Execute(ctx context.Context, serviceName string, fn func() (interface{}, error)) (interface{}, error) {
	if serviceName == "" {
		return nil, ErrServiceNameEmpty
	}

	// 获取或创建服务级熔断器
	breaker := cb.getOrCreateBreaker(serviceName)

	// 记录请求开始时间
	start := time.Now()

	// 执行熔断保护的函数
	result, err := breaker.Execute(fn)

	// 记录请求耗时
	duration := time.Since(start)

	// 记录指标
	cb.recordMetrics(ctx, serviceName, err, duration)

	// 如果熔断器打开且配置了降级函数
	if err != nil && xerrors.Is(err, gobreaker.ErrOpenState) {
		if cb.logger != nil {
			cb.logger.Warn("circuit breaker open",
				clog.String("service", serviceName),
				clog.Error(err))
		}

		// 执行降级逻辑
		if cb.fallback != nil {
			fallbackErr := cb.fallback(ctx, serviceName, err)
			if fallbackErr == nil {
				return nil, nil
			}
			return nil, fallbackErr
		}

		return nil, ErrOpenState
	}

	return result, err
}

// State 获取指定服务的熔断器状态
func (cb *circuitBreaker) State(serviceName string) (State, error) {
	if serviceName == "" {
		return StateClosed, ErrServiceNameEmpty
	}

	val, ok := cb.breakers.Load(serviceName)
	if !ok {
		return StateClosed, ErrBreakerNotFound
	}

	breaker := val.(*gobreaker.CircuitBreaker[interface{}])
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

// getOrCreateBreaker 获取或创建服务级熔断器
func (cb *circuitBreaker) getOrCreateBreaker(serviceName string) *gobreaker.CircuitBreaker[interface{}] {
	val, ok := cb.breakers.Load(serviceName)
	if ok {
		return val.(*gobreaker.CircuitBreaker[interface{}])
	}

	// 创建新的熔断器
	settings := gobreaker.Settings{
		Name:        serviceName,
		MaxRequests: cb.cfg.MaxRequests,
		Interval:    cb.cfg.Interval,
		Timeout:     cb.cfg.Timeout,
		ReadyToTrip: cb.readyToTrip,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			cb.onStateChange(name, from, to)
		},
	}

	breaker := gobreaker.NewCircuitBreaker[interface{}](settings)

	// 存储熔断器（可能有并发创建，使用 LoadOrStore）
	actual, _ := cb.breakers.LoadOrStore(serviceName, breaker)
	return actual.(*gobreaker.CircuitBreaker[interface{}])
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
func (cb *circuitBreaker) onStateChange(name string, from gobreaker.State, to gobreaker.State) {
	if cb.logger != nil {
		cb.logger.Info("circuit breaker state changed",
			clog.String("service", name),
			clog.String("from", stateToString(from)),
			clog.String("to", stateToString(to)))
	}

	// 记录状态变更指标
	if cb.meter != nil {
		counter, err := cb.meter.Counter(MetricStateChanges, "Circuit breaker state changes")
		if err == nil && counter != nil {
			counter.Add(context.Background(), 1,
				metrics.L(LabelService, name),
				metrics.L(LabelFromState, stateToString(from)),
				metrics.L(LabelToState, stateToString(to)))
		}
	}
}

// recordMetrics 记录指标
func (cb *circuitBreaker) recordMetrics(ctx context.Context, serviceName string, err error, duration time.Duration) {
	if cb.meter == nil {
		return
	}

	// 记录请求总数
	if counter, e := cb.meter.Counter(MetricRequestsTotal, "Total requests"); e == nil && counter != nil {
		counter.Add(ctx, 1, metrics.L(LabelService, serviceName))
	}

	// 记录请求耗时
	if histogram, e := cb.meter.Histogram(MetricRequestDuration, "Request duration", metrics.WithUnit("seconds")); e == nil && histogram != nil {
		histogram.Record(ctx, duration.Seconds(), metrics.L(LabelService, serviceName))
	}

	// 记录成功/失败/拒绝
	if err == nil {
		// 成功
		if counter, e := cb.meter.Counter(MetricSuccessTotal, "Successful requests"); e == nil && counter != nil {
			counter.Add(ctx, 1, metrics.L(LabelService, serviceName))
		}
	} else if xerrors.Is(err, gobreaker.ErrOpenState) || xerrors.Is(err, gobreaker.ErrTooManyRequests) {
		// 被熔断拒绝
		if counter, e := cb.meter.Counter(MetricRejectsTotal, "Rejected requests"); e == nil && counter != nil {
			counter.Add(ctx, 1, metrics.L(LabelService, serviceName))
		}
	} else {
		// 失败
		if counter, e := cb.meter.Counter(MetricFailuresTotal, "Failed requests"); e == nil && counter != nil {
			counter.Add(ctx, 1, metrics.L(LabelService, serviceName))
		}
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
