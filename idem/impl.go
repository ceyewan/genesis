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

	// 尝试获取缓存结果
	cachedResult, err := i.store.GetResult(ctx, key)
	if err == nil {
		// 缓存命中
		if i.logger != nil {
			i.logger.Debug("idem cache hit", clog.String("key", key))
		}
		// 反序列化结果
		var result interface{}
		if err := json.Unmarshal(cachedResult, &result); err != nil {
			if i.logger != nil {
				i.logger.Error("failed to unmarshal cached result", clog.Error(err), clog.String("key", key))
			}
			return nil, xerrors.Wrap(err, "failed to unmarshal cached result")
		}
		return result, nil
	}

	if err != ErrResultNotFound {
		// 存储错误
		if i.logger != nil {
			i.logger.Error("failed to get cached result", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}

	// 缓存未命中，尝试获取锁
	locked, err := i.store.Lock(ctx, key, i.cfg.LockTTL)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to acquire lock", clog.Error(err), clog.String("key", key))
		}
		return nil, err
	}

	if !locked {
		// 未获取到锁，说明有并发请求
		if i.logger != nil {
			i.logger.Debug("concurrent request detected", clog.String("key", key))
		}
		return nil, ErrConcurrentRequest
	}

	lockReleased := false
	defer func() {
		if lockReleased {
			return
		}
		if err := i.store.Unlock(ctx, key); err != nil && i.logger != nil {
			i.logger.Error("failed to unlock after execution failure", clog.Error(err), clog.String("key", key))
		}
	}()

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
	if err := i.store.SetResult(ctx, key, resultBytes, i.cfg.DefaultTTL); err != nil {
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

	locked, err := i.store.Lock(ctx, key, i.cfg.LockTTL)
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
		if err := i.store.Unlock(ctx, key); err != nil && i.logger != nil {
			i.logger.Error("failed to unlock after consume failure", clog.Error(err), clog.String("key", key))
		}
	}()

	if err := fn(ctx); err != nil {
		if i.logger != nil {
			i.logger.Error("consume execution failed", clog.Error(err), clog.String("key", key))
		}
		return false, err
	}

	if err := i.store.SetResult(ctx, key, []byte(processedMarker), ttl); err != nil {
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
