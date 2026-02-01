package idem

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// idem 幂等性组件实现（非导出）
type idem struct {
	cfg    *Config
	store  Store
	logger clog.Logger
}

const processedMarker = "1"

// newIdempotency 创建幂等性组件实例（内部函数）
func newIdempotency(cfg *Config, store Store, logger clog.Logger) Idempotency {
	return &idem{
		cfg:    cfg,
		store:  store,
		logger: logger,
	}
}

// Execute 执行幂等操作
func (i *idem) Execute(ctx context.Context, key string, fn func(ctx context.Context) (interface{}, error)) (interface{}, error) {
	if key == "" {
		return nil, ErrKeyEmpty
	}

	cachedResult, token, locked, err := i.waitForResultOrLock(ctx, key)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to wait for result or lock", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}
	if !locked {
		if i.logger != nil {
			i.logger.Debug("idem cache hit", clog.String("key", key))
		}
		return decodeJSONResult(cachedResult, i.logger, key)
	}

	lockReleased := false
	defer func() {
		if lockReleased {
			return
		}
		if err := i.store.Unlock(ctx, key, token); err != nil && i.logger != nil {
			i.logger.Error("failed to unlock after execution failure", clog.Error(err), clog.String("key", key))
		}
	}()
	stopRefresh := i.startLockRefresh(key, token)
	defer stopRefresh()

	// 执行业务逻辑
	result, err := fn(ctx)

	// 处理执行结果
	if err != nil {
		if i.logger != nil {
			i.logger.Error("execution failed", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}

	// 执行成功，序列化并保存结果
	resultBytes, err := json.Marshal(result)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to marshal result", clog.Error(err), clog.String("key", key))
		}
		return nil, xerrors.Wrap(err, "failed to marshal result")
	}

	// 保存结果
	if err := i.store.SetResult(ctx, key, resultBytes, i.cfg.DefaultTTL, token); err != nil {
		if i.logger != nil {
			i.logger.Error("failed to set result", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}
	lockReleased = true

	if i.logger != nil {
		i.logger.Debug("execution completed and cached", clog.String("key", key))
	}

	return result, nil
}

// Consume 用于消息消费的幂等处理
func (i *idem) Consume(ctx context.Context, key string, ttl time.Duration, fn func(ctx context.Context) error) (bool, error) {
	if key == "" {
		return false, ErrKeyEmpty
	}

	if ttl <= 0 {
		ttl = i.cfg.DefaultTTL
	}

	_, err := i.store.GetResult(ctx, key)
	if err == nil {
		if i.logger != nil {
			i.logger.Debug("idem consume hit", clog.String("key", key))
		}
		return false, nil
	}
	if err != ErrResultNotFound {
		if i.logger != nil {
			i.logger.Error("failed to get consume marker", clog.Error(err), clog.String("key", key))
		}
		return false, err
	}

	token, locked, err := i.store.Lock(ctx, key, i.cfg.LockTTL)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to acquire consume lock", clog.Error(err), clog.String("key", key))
		}
		return false, err
	}
	if !locked {
		if i.logger != nil {
			i.logger.Debug("concurrent consume detected", clog.String("key", key))
		}
		return false, ErrConcurrentRequest
	}

	lockReleased := false
	defer func() {
		if lockReleased {
			return
		}
		if err := i.store.Unlock(ctx, key, token); err != nil && i.logger != nil {
			i.logger.Error("failed to unlock after consume failure", clog.Error(err), clog.String("key", key))
		}
	}()
	stopRefresh := i.startLockRefresh(key, token)
	defer stopRefresh()

	if err := fn(ctx); err != nil {
		if i.logger != nil {
			i.logger.Error("consume execution failed", clog.Error(err), clog.String("key", key))
		}
		return false, err
	}

	if err := i.store.SetResult(ctx, key, []byte(processedMarker), ttl, token); err != nil {
		if i.logger != nil {
			i.logger.Error("failed to set consume marker", clog.Error(err), clog.String("key", key))
		}
		return false, err
	}
	lockReleased = true

	if i.logger != nil {
		i.logger.Debug("consume completed", clog.String("key", key))
	}

	return true, nil
}

func (i *idem) waitForResultOrLock(ctx context.Context, key string) ([]byte, LockToken, bool, error) {
	waitCtx, cancel := i.withWaitTimeout(ctx)
	defer cancel()

	interval := i.cfg.WaitInterval
	if interval <= 0 {
		interval = 50 * time.Millisecond
	}
	maxInterval := 500 * time.Millisecond

	for {
		if err := waitCtx.Err(); err != nil {
			return nil, "", false, err
		}

		cached, err := i.store.GetResult(waitCtx, key)
		if err == nil {
			return cached, "", false, nil
		}
		if err != ErrResultNotFound {
			return nil, "", false, err
		}

		token, locked, err := i.store.Lock(waitCtx, key, i.cfg.LockTTL)
		if err != nil {
			return nil, "", false, err
		}
		if locked {
			return nil, token, true, nil
		}

		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return nil, "", false, waitCtx.Err()
		case <-timer.C:
		}
		if interval < maxInterval {
			interval = interval * 2
			if interval > maxInterval {
				interval = maxInterval
			}
		}
	}
}

func (i *idem) withWaitTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if i.cfg.WaitTimeout <= 0 {
		return ctx, func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		if time.Until(deadline) <= i.cfg.WaitTimeout {
			return ctx, func() {}
		}
	}
	return context.WithTimeout(ctx, i.cfg.WaitTimeout)
}

func (i *idem) startLockRefresh(key string, token LockToken) func() {
	rs, ok := i.store.(RefreshableStore)
	if !ok || i.cfg.LockTTL <= 0 || token == "" {
		return func() {}
	}

	interval := i.cfg.LockTTL / 2
	if interval < 500*time.Millisecond {
		interval = 500 * time.Millisecond
	}

	stopCtx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := rs.Refresh(stopCtx, key, token, i.cfg.LockTTL); err != nil && i.logger != nil {
					i.logger.Warn("failed to refresh lock", clog.Error(err), clog.String("key", key))
				}
			case <-stopCtx.Done():
				return
			}
		}
	}()

	return cancel
}

func decodeJSONResult(cached []byte, logger clog.Logger, key string) (interface{}, error) {
	var result interface{}
	if err := json.Unmarshal(cached, &result); err != nil {
		if logger != nil {
			logger.Error("failed to unmarshal cached result", clog.Error(err), clog.String("key", key))
		}
		return nil, xerrors.Wrap(err, "failed to unmarshal cached result")
	}
	return result, nil
}
