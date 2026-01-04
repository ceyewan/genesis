package ratelimit

import (
	"context"
	"fmt"
	"sync"
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
	mu       sync.Mutex
}

// standaloneLimiter 单机限流器实现（非导出）
type standaloneLimiter struct {
	cfg      *StandaloneConfig
	logger   clog.Logger
	meter    metrics.Meter
	limiters sync.Map // map[string]*limiterWrapper
	stopCh   chan struct{}
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

	l := &standaloneLimiter{
		cfg:    cfg,
		logger: logger,
		meter:  meter,
		stopCh: make(chan struct{}),
	}

	// 启动清理 goroutine
	cleanupInterval := cfg.CleanupInterval
	idleTimeout := cfg.IdleTimeout

	go l.cleanup(cleanupInterval, idleTimeout)

	if logger != nil {
		logger.Info("standalone rate limiter created",
			clog.Duration("cleanup_interval", cleanupInterval),
			clog.Duration("idle_timeout", idleTimeout))
	}

	return l, nil
}

// Allow 尝试获取 1 个令牌
func (l *standaloneLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
	return l.AllowN(ctx, key, limit, 1)
}

// AllowN 尝试获取 N 个令牌
func (l *standaloneLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error) {
	if key == "" {
		return false, ErrKeyEmpty
	}

	if limit.Rate <= 0 || limit.Burst <= 0 {
		return false, ErrInvalidLimit
	}

	if n <= 0 {
		return false, xerrors.Wrapf(xerrors.ErrInvalidInput, "ratelimit: n must be positive")
	}

	// 获取或创建 limiter
	wrapper := l.getLimiter(key, limit)

	// 尝试获取令牌
	wrapper.mu.Lock()
	allowed := wrapper.limiter.AllowN(time.Now(), n)
	wrapper.lastSeen = time.Now()
	wrapper.mu.Unlock()

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
func (l *standaloneLimiter) Wait(ctx context.Context, key string, limit Limit) error {
	if key == "" {
		return ErrKeyEmpty
	}

	if limit.Rate <= 0 || limit.Burst <= 0 {
		return ErrInvalidLimit
	}

	// 获取或创建 limiter
	wrapper := l.getLimiter(key, limit)

	// 等待直到获取令牌
	wrapper.mu.Lock()
	err := wrapper.limiter.Wait(ctx)
	wrapper.lastSeen = time.Now()
	wrapper.mu.Unlock()

	if l.logger != nil {
		l.logger.Debug("rate limit wait",
			clog.String("key", key),
			clog.Float64("rate", limit.Rate),
			clog.Int("burst", limit.Burst))
	}

	return err
}

// getLimiter 获取或创建指定 key 的限流器
func (l *standaloneLimiter) getLimiter(key string, limit Limit) *limiterWrapper {
	// 构造缓存 key (包含 rate 和 burst)
	cacheKey := fmt.Sprintf("%s:%v:%d", key, limit.Rate, limit.Burst)

	// 尝试从缓存获取
	if v, ok := l.limiters.Load(cacheKey); ok {
		return v.(*limiterWrapper)
	}

	// 创建新的限流器
	wrapper := &limiterWrapper{
		limiter:  rate.NewLimiter(rate.Limit(limit.Rate), limit.Burst),
		lastSeen: time.Now(),
	}

	// 存储到缓存 (如果已存在则使用已存在的)
	actual, _ := l.limiters.LoadOrStore(cacheKey, wrapper)
	return actual.(*limiterWrapper)
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

			l.limiters.Range(func(key, value interface{}) bool {
				wrapper := value.(*limiterWrapper)
				wrapper.mu.Lock()
				idle := now.Sub(wrapper.lastSeen)
				wrapper.mu.Unlock()

				if idle > idleTimeout {
					l.limiters.Delete(key)
					count++
				}
				return true
			})

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
	close(l.stopCh)
	return nil
}
