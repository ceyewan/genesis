package idempotency

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ceyewan/genesis/internal/idempotency/store"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/idempotency/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Idempotent 幂等组件实现
type Idempotent struct {
	store  *store.RedisStore
	cfg    *types.Config
	logger clog.Logger
	meter  telemetrytypes.Meter
	tracer telemetrytypes.Tracer
}

// New 创建幂等组件实例
func New(
	redisConn connector.RedisConnector,
	cfg *types.Config,
	logger clog.Logger,
	meter telemetrytypes.Meter,
	tracer telemetrytypes.Tracer,
) (*Idempotent, error) {
	if redisConn == nil {
		return nil, fmt.Errorf("redis connector is nil")
	}

	// 使用默认配置
	if cfg == nil {
		cfg = types.DefaultConfig()
	}

	// 派生 Logger (添加 "idempotency" namespace)
	if logger != nil {
		logger = logger.WithNamespace("idempotency")
	}

	// 创建 Redis Store
	redisStore := store.NewRedisStore(redisConn, cfg.Prefix)

	return &Idempotent{
		store:  redisStore,
		cfg:    cfg,
		logger: logger,
		meter:  meter,
		tracer: tracer,
	}, nil
}

// Do 执行幂等操作
func (i *Idempotent) Do(ctx context.Context, key string, fn func() (any, error), opts ...types.DoOption) (any, error) {
	if key == "" {
		return nil, types.ErrKeyEmpty
	}

	// 应用选项
	opt := types.DoOptions{
		TTL: i.cfg.DefaultTTL,
	}
	for _, o := range opts {
		o(&opt)
	}

	// 1. 尝试加锁 (SET NX)
	locked, status, err := i.store.Lock(ctx, key, i.cfg.ProcessingTTL)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to lock", clog.String("key", key), clog.Error(err))
		}
		return nil, fmt.Errorf("lock failed: %w", err)
	}

	// 2. 如果加锁失败，检查状态
	if !locked {
		if status == types.StatusSuccess {
			// 3. 状态为 Success，返回缓存结果
			_, resultJSON, err := i.store.Get(ctx, key)
			if err != nil {
				if i.logger != nil {
					i.logger.Error("failed to get cached result", clog.String("key", key), clog.Error(err))
				}
				return nil, fmt.Errorf("get cached result: %w", err)
			}

			// 反序列化结果
			var result any
			if resultJSON != "" {
				if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
					if i.logger != nil {
						i.logger.Error("failed to unmarshal result", clog.String("key", key), clog.Error(err))
					}
					return nil, fmt.Errorf("unmarshal result: %w", err)
				}
			}

			if i.logger != nil {
				i.logger.Info("return cached result", clog.String("key", key))
			}
			return result, nil
		}

		if status == types.StatusProcessing {
			// 4. 状态为 Processing，返回 ErrProcessing
			if i.logger != nil {
				i.logger.Warn("request is being processed", clog.String("key", key))
			}
			return nil, types.ErrProcessing
		}

		// StatusFailed: 这种情况不应该发生，因为 Lock 失败且状态为 Failed 时，
		// 应该允许重试（Key 应该被删除或过期）
		if i.logger != nil {
			i.logger.Warn("unexpected status", clog.String("key", key), clog.Any("status", status))
		}
		return nil, fmt.Errorf("unexpected status: %v", status)
	}

	// 5. 执行业务逻辑
	if i.logger != nil {
		i.logger.Info("executing business logic", clog.String("key", key))
	}

	result, err := fn()

	// 6. 处理结果
	if err != nil {
		// 业务失败：删除 Key (允许重试)
		if delErr := i.store.Delete(ctx, key); delErr != nil {
			if i.logger != nil {
				i.logger.Error("failed to delete key after business error",
					clog.String("key", key),
					clog.Error(delErr))
			}
		}

		if i.logger != nil {
			i.logger.Error("business logic failed", clog.String("key", key), clog.Error(err))
		}
		return nil, err
	}

	// 业务成功：序列化结果
	var resultJSON string
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			// 序列化失败，删除 Key
			if delErr := i.store.Delete(ctx, key); delErr != nil {
				if i.logger != nil {
					i.logger.Error("failed to delete key after marshal error",
						clog.String("key", key),
						clog.Error(delErr))
				}
			}
			if i.logger != nil {
				i.logger.Error("failed to marshal result", clog.String("key", key), clog.Error(err))
			}
			return nil, fmt.Errorf("marshal result: %w", err)
		}
		resultJSON = string(data)
	}

	// 更新状态并缓存结果
	if err := i.store.Unlock(ctx, key, types.StatusSuccess, resultJSON, opt.TTL); err != nil {
		if i.logger != nil {
			i.logger.Error("failed to unlock and save result",
				clog.String("key", key),
				clog.Error(err))
		}
		return nil, fmt.Errorf("unlock failed: %w", err)
	}

	if i.logger != nil {
		i.logger.Info("business logic succeeded", clog.String("key", key))
	}
	return result, nil
}

// Check 检查幂等键的状态
func (i *Idempotent) Check(ctx context.Context, key string) (types.Status, any, error) {
	if key == "" {
		return 0, nil, types.ErrKeyEmpty
	}

	// 从 Redis 获取记录
	status, resultJSON, err := i.store.Get(ctx, key)
	if err != nil {
		if i.logger != nil {
			i.logger.Error("failed to check status", clog.String("key", key), clog.Error(err))
		}
		return 0, nil, err
	}

	// 解析结果
	var result any
	if resultJSON != "" && status == types.StatusSuccess {
		if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
			if i.logger != nil {
				i.logger.Error("failed to unmarshal result", clog.String("key", key), clog.Error(err))
			}
			return status, nil, fmt.Errorf("unmarshal result: %w", err)
		}
	}

	if i.logger != nil {
		i.logger.Info("check status", clog.String("key", key), clog.Any("status", status))
	}
	return status, result, nil
}

// Delete 删除幂等记录
func (i *Idempotent) Delete(ctx context.Context, key string) error {
	if key == "" {
		return types.ErrKeyEmpty
	}

	if err := i.store.Delete(ctx, key); err != nil {
		if i.logger != nil {
			i.logger.Error("failed to delete key", clog.String("key", key), clog.Error(err))
		}
		return err
	}

	if i.logger != nil {
		i.logger.Info("deleted idempotency record", clog.String("key", key))
	}
	return nil
}
