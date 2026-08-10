package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// limiterWrapper 包装 rate.Limiter 并记录最后访问时间
type limiterWrapper struct {
	limiter  *rate.Limiter
	lastSeen time.Time
	active   int
}

// standaloneLimiter 单机限流器实现（非导出）
type standaloneLimiter struct {
	cfg       *StandaloneConfig
	logger    clog.Logger
	limiters  map[string]*limiterWrapper
	limiterMu sync.Mutex
	maxKeys   int
	stopCh    chan struct{}
	closeOnce sync.Once
	workerWG  sync.WaitGroup
	closed    atomic.Bool

	// 指标
	allowedCounter metrics.Counter
	deniedCounter  metrics.Counter
	errorCounter   metrics.Counter
}

// newStandalone 创建单机限流器（内部函数）
func newStandalone(
	cfg *StandaloneConfig,
	logger clog.Logger,
	meter metrics.Meter,
) (Limiter, error) {
	if cfg == nil {
		cfg = &StandaloneConfig{}
	}
	cfg.setDefaults()
	if cfg.CleanupInterval < 0 || cfg.IdleTimeout < 0 || cfg.MaxKeys < 1 {
		return nil, xerrors.Wrap(ErrInvalidConfig, "invalid standalone configuration")
	}

	l := &standaloneLimiter{
		cfg:      cfg,
		logger:   logger,
		limiters: make(map[string]*limiterWrapper),
		maxKeys:  cfg.MaxKeys,
		stopCh:   make(chan struct{}),
	}

	// 初始化指标
	if meter != nil {
		l.allowedCounter, _ = meter.Counter(MetricAllowed, "Number of allowed requests")
		l.deniedCounter, _ = meter.Counter(MetricDenied, "Number of denied requests")
		l.errorCounter, _ = meter.Counter(MetricErrors, "Number of limiter operation errors")
	}

	// 启动清理 goroutine
	cleanupInterval := cfg.CleanupInterval
	idleTimeout := cfg.IdleTimeout

	l.workerWG.Go(func() {
		l.cleanup(cleanupInterval, idleTimeout)
	})

	if logger != nil {
		logger.Info("standalone rate limiter created",
			clog.Duration("cleanup_interval", cleanupInterval),
			clog.Duration("idle_timeout", idleTimeout),
			clog.Int("max_keys", cfg.MaxKeys))
	}

	return l, nil
}

// Allow 尝试获取 1 个令牌
func (l *standaloneLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
	return l.AllowN(ctx, key, limit, 1)
}

// AllowN 尝试获取 N 个令牌
func (l *standaloneLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (allowed bool, err error) {
	defer func() {
		if err != nil {
			recordLimiterError(ctx, l.errorCounter, "standalone")
		}
	}()
	if l.closed.Load() {
		return false, ErrLimiterClosed
	}
	if key == "" {
		return false, ErrKeyEmpty
	}

	if !limit.valid() {
		return false, ErrInvalidLimit
	}

	if n <= 0 {
		return false, ErrInvalidLimit
	}

	// 获取或创建 limiter。active 计数防止清理协程删除正在使用的桶。
	cacheKey, wrapper, err := l.acquireLimiter(key, limit)
	if err != nil {
		return false, err
	}
	defer l.releaseLimiter(cacheKey, wrapper)

	// 尝试获取令牌
	allowed = wrapper.limiter.AllowN(time.Now(), n)

	// 记录指标
	if allowed {
		if l.allowedCounter != nil {
			l.allowedCounter.Inc(ctx, metrics.L(LabelMode, "standalone"))
		}
	} else {
		if l.deniedCounter != nil {
			l.deniedCounter.Inc(ctx, metrics.L(LabelMode, "standalone"))
		}
	}

	if l.logger != nil {
		l.logger.Debug("rate limit check",
			clog.String("key", key),
			clog.Bool("allowed", allowed),
			clog.Float64("rate", limit.Rate),
			clog.Int("burst", limit.Burst),
			clog.Int("requested", n))
	}

	return allowed, nil
}

// Wait 阻塞等待直到获取 1 个令牌
func (l *standaloneLimiter) Wait(ctx context.Context, key string, limit Limit) (err error) {
	defer func() {
		if err != nil {
			recordLimiterError(ctx, l.errorCounter, "standalone")
		}
	}()
	if l.closed.Load() {
		return ErrLimiterClosed
	}
	if key == "" {
		return ErrKeyEmpty
	}

	if !limit.valid() {
		return ErrInvalidLimit
	}

	// 获取或创建 limiter。Wait 阻塞期间保持 active，避免清理后重建出新桶。
	cacheKey, wrapper, err := l.acquireLimiter(key, limit)
	if err != nil {
		return err
	}
	defer l.releaseLimiter(cacheKey, wrapper)

	// 等待直到获取令牌
	err = wrapper.limiter.Wait(ctx)

	if l.logger != nil {
		l.logger.Debug("rate limit wait",
			clog.String("key", key),
			clog.Float64("rate", limit.Rate),
			clog.Int("burst", limit.Burst))
	}

	return err
}

// acquireLimiter 获取或创建指定 key 的限流器，并标记一次活跃使用。
func (l *standaloneLimiter) acquireLimiter(key string, limit Limit) (string, *limiterWrapper, error) {
	// 构造缓存 key (包含 rate 和 burst)
	cacheKey := fmt.Sprintf("%s:%v:%d", key, limit.Rate, limit.Burst)
	now := time.Now()

	l.limiterMu.Lock()
	defer l.limiterMu.Unlock()

	if wrapper, ok := l.limiters[cacheKey]; ok {
		wrapper.active++
		wrapper.lastSeen = now
		return cacheKey, wrapper, nil
	}
	if len(l.limiters) >= l.maxKeys {
		return "", nil, ErrKeyLimitExceeded
	}

	wrapper := &limiterWrapper{
		limiter:  rate.NewLimiter(rate.Limit(limit.Rate), limit.Burst),
		lastSeen: now,
		active:   1,
	}
	l.limiters[cacheKey] = wrapper
	return cacheKey, wrapper, nil
}

func (l *standaloneLimiter) releaseLimiter(cacheKey string, wrapper *limiterWrapper) {
	l.limiterMu.Lock()
	defer l.limiterMu.Unlock()
	if current, ok := l.limiters[cacheKey]; ok && current == wrapper {
		wrapper.active--
		wrapper.lastSeen = time.Now()
	}
}

// cleanup 定期清理过期的限流器
func (l *standaloneLimiter) cleanup(interval, idleTimeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			count := 0

			l.limiterMu.Lock()
			for key, wrapper := range l.limiters {
				if wrapper.active == 0 && now.Sub(wrapper.lastSeen) > idleTimeout {
					delete(l.limiters, key)
					count++
				}
			}
			l.limiterMu.Unlock()

			if count > 0 && l.logger != nil {
				l.logger.Debug("cleaned up idle limiters", clog.Int("count", count))
			}

		case <-l.stopCh:
			return
		}
	}
}

// Close 关闭限流器
func (l *standaloneLimiter) Close() error {
	l.closed.Store(true)
	l.closeOnce.Do(func() {
		close(l.stopCh)
	})
	l.workerWG.Wait()
	return nil
}
