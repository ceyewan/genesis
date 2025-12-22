package idempotency

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// idempotency 幂等性组件实现（非导出）
type idempotency struct {
	cfg    *Config
	store  Store
	logger clog.Logger
	meter  metrics.Meter
}

// newIdempotency 创建幂等性组件实例（内部函数）
func newIdempotency(
	cfg *Config,
	redisConn connector.RedisConnector,
	logger clog.Logger,
	meter metrics.Meter,
) (Idempotency, error) {
	// 设置默认值
	if cfg.Prefix == "" {
		cfg.Prefix = "idempotency:"
	}
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 24 * time.Hour
	}
	if cfg.LockTTL == 0 {
		cfg.LockTTL = 30 * time.Second
	}

	// 创建 Redis 存储
	store := newRedisStore(redisConn, cfg.Prefix)

	return &idempotency{
		cfg:    cfg,
		store:  store,
		logger: logger,
		meter:  meter,
	}, nil
}

// Execute 执行幂等操作
func (i *idempotency) Execute(ctx context.Context, key string, fn func(ctx context.Context) (interface{}, error)) (interface{}, error) {
	if key == "" {
		return nil, ErrKeyEmpty
	}

	// 尝试获取缓存结果
	cachedResult, err := i.store.GetResult(ctx, key)
	if err == nil {
		// 缓存命中
		if i.logger != nil {
			i.logger.Debug("idempotency cache hit", clog.String("key", key))
		}
		i.recordMetric(ctx, "cache_hit")

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
		i.recordMetric(ctx, "storage_error")
		return nil, err
	}

	// 缓存未命中，尝试获取锁
	lockStart := time.Now()
	locked, err := i.store.Lock(ctx, key, i.cfg.LockTTL)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to acquire lock", clog.Error(err), clog.String("key", key))
		}
		i.recordMetric(ctx, "storage_error")
		return nil, err
	}

	if !locked {
		// 未获取到锁，说明有并发请求
		if i.logger != nil {
			i.logger.Debug("concurrent request detected", clog.String("key", key))
		}
		i.recordMetric(ctx, "concurrent")

		// 根据配置决定是否等待
		if i.cfg.WaitTimeout == 0 {
			return nil, ErrConcurrentRequest
		}

		// 等待结果完成
		result, err := i.store.WaitForResult(ctx, key, i.cfg.WaitTimeout)
		if err != nil {
			if i.logger != nil {
				i.logger.Warn("wait for result failed", clog.Error(err), clog.String("key", key))
			}
			return nil, err
		}

		// 反序列化结果
		var res interface{}
		if err := json.Unmarshal(result, &res); err != nil {
			if i.logger != nil {
				i.logger.Error("failed to unmarshal waited result", clog.Error(err), clog.String("key", key))
			}
			return nil, xerrors.Wrap(err, "failed to unmarshal waited result")
		}
		return res, nil
	}

	// 记录锁获取耗时
	if i.meter != nil {
		if histogram, err := i.meter.Histogram(MetricLockAcquisitionDuration, "Lock acquisition duration"); err == nil && histogram != nil {
			histogram.Record(ctx, time.Since(lockStart).Seconds())
		}
	}

	// 执行业务逻辑
	execStart := time.Now()
	result, err := fn(ctx)

	// 记录执行耗时
	if i.meter != nil {
		if histogram, err := i.meter.Histogram(MetricExecutionDuration, "Execution duration"); err == nil && histogram != nil {
			histogram.Record(ctx, time.Since(execStart).Seconds())
		}
	}

	// 处理执行结果
	if err != nil {
		// 执行失败，释放锁
		if unlockErr := i.store.Unlock(ctx, key); unlockErr != nil {
			if i.logger != nil {
				i.logger.Error("failed to unlock after execution failure", clog.Error(unlockErr), clog.String("key", key))
			}
		}
		if i.logger != nil {
			i.logger.Error("execution failed", clog.Error(err), clog.String("key", key))
		}
		i.recordMetric(ctx, "failure")
		return nil, err
	}

	// 执行成功，序列化并保存结果
	resultBytes, err := json.Marshal(result)
	if err != nil {
		// 序列化失败，释放锁
		if unlockErr := i.store.Unlock(ctx, key); unlockErr != nil {
			if i.logger != nil {
				i.logger.Error("failed to unlock after marshal failure", clog.Error(unlockErr), clog.String("key", key))
			}
		}
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
		i.recordMetric(ctx, "storage_error")
		return nil, err
	}

	if i.logger != nil {
		i.logger.Debug("execution completed and cached", clog.String("key", key))
	}
	i.recordMetric(ctx, "success")

	return result, nil
}

// recordMetric 记录指标
func (i *idempotency) recordMetric(ctx context.Context, operation string) {
	if i.meter == nil {
		return
	}

	if counter, err := i.meter.Counter(MetricExecutionsTotal, "Total executions"); err == nil && counter != nil {
		counter.Add(ctx, 1, metrics.L(LabelOperation, operation))
	}
}
