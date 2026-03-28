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
func (i *idem) Execute(ctx context.Context, key string, fn func(ctx context.Context) (any, error)) (any, error) {
	if key == "" {
		return nil, ErrKeyEmpty
	}

	cachedResult, token, locked, err := i.loadResultOrAcquireLock(ctx, key, decodeJSONResult)
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
		return cachedResult, nil
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
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopRefresh, refreshErrCh := i.startLockRefresh(key, token, cancel)
	defer stopRefresh()

	// 执行业务逻辑
	result, err := fn(execCtx)

	// 处理执行结果
	if err != nil {
		if i.logger != nil {
			i.logger.Error("execution failed", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}

	if refreshErr := collectRefreshError(refreshErrCh); refreshErr != nil {
		if i.logger != nil {
			i.logger.Error("lock refresh failed during execution", clog.Error(refreshErr), clog.String("key", key))
		}
		return nil, refreshErr
	}

	normalizedResult, resultBytes, err := normalizeJSONResult(result, i.logger, key)
	if err != nil {
		return nil, err
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

	return normalizedResult, nil
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
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopRefresh, refreshErrCh := i.startLockRefresh(key, token, cancel)
	defer stopRefresh()

	if err := fn(execCtx); err != nil {
		if i.logger != nil {
			i.logger.Error("consume execution failed", clog.Error(err), clog.String("key", key))
		}
		return false, err
	}

	if refreshErr := collectRefreshError(refreshErrCh); refreshErr != nil {
		if i.logger != nil {
			i.logger.Error("lock refresh failed during consume", clog.Error(refreshErr), clog.String("key", key))
		}
		return false, refreshErr
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

type cachedResultDecoder func(cached []byte, logger clog.Logger, key string) (any, error)

func (i *idem) loadResultOrAcquireLock(ctx context.Context, key string, decode cachedResultDecoder) (any, LockToken, bool, error) {
	for range 2 {
		cachedResult, token, locked, err := i.waitForResultOrLock(ctx, key)
		if err != nil {
			return nil, "", false, err
		}
		if locked {
			return nil, token, true, nil
		}

		result, err := decode(cachedResult, i.logger, key)
		if err == nil {
			return result, "", false, nil
		}

		if deleteErr := i.deleteCorruptedResult(ctx, key); deleteErr != nil {
			return nil, "", false, deleteErr
		}
	}

	return nil, "", false, xerrors.New("failed to recover corrupted cached result")
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
			interval = min(interval*2, maxInterval)
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

func (i *idem) startLockRefresh(key string, token LockToken, onFailure context.CancelFunc) (func(), <-chan error) {
	rs, ok := i.store.(RefreshableStore)
	if !ok || i.cfg.LockTTL <= 0 || token == "" {
		return func() {}, nil
	}

	interval := max(i.cfg.LockTTL/2, 500*time.Millisecond)

	stopCtx, stop := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	ticker := time.NewTicker(interval)
	go func() {
		defer close(errCh)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := rs.Refresh(stopCtx, key, token, i.cfg.LockTTL); err != nil {
					select {
					case errCh <- err:
					default:
					}
					onFailure()
					return
				}
			case <-stopCtx.Done():
				return
			}
		}
	}()

	return stop, errCh
}

func decodeJSONResult(cached []byte, logger clog.Logger, key string) (any, error) {
	var result any
	if err := json.Unmarshal(cached, &result); err != nil {
		if logger != nil {
			logger.Error("failed to unmarshal cached result", clog.Error(err), clog.String("key", key))
		}
		return nil, xerrors.Wrap(err, "failed to unmarshal cached result")
	}
	return result, nil
}

func normalizeJSONResult(result any, logger clog.Logger, key string) (any, []byte, error) {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		if logger != nil {
			logger.Error("failed to marshal result", clog.Error(err), clog.String("key", key))
		}
		return nil, nil, xerrors.Wrap(err, "failed to marshal result")
	}

	normalizedResult, err := decodeJSONResult(resultBytes, logger, key)
	if err != nil {
		return nil, nil, err
	}

	return normalizedResult, resultBytes, nil
}

func (i *idem) deleteCorruptedResult(ctx context.Context, key string) error {
	ds, ok := i.store.(DeletableStore)
	if !ok {
		return ErrResultNotFound
	}
	if err := ds.DeleteResult(ctx, key); err != nil {
		if i.logger != nil {
			i.logger.Error("failed to delete corrupted cached result", clog.Error(err), clog.String("key", key))
		}
		return err
	}
	if i.logger != nil {
		i.logger.Warn("deleted corrupted cached result", clog.String("key", key))
	}
	return nil
}

func collectRefreshError(errCh <-chan error) error {
	if errCh == nil {
		return nil
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}
