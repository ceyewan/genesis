package idgen

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

// ========================================
// Redis 实现 (Redis Implementation)
// ========================================

// redisSequencer Redis 实现的序列号生成器
type redisSequencer struct {
	redis  connector.RedisConnector
	cfg    *SequenceConfig
	logger clog.Logger
	meter  metrics.Meter
}

// NewSequencer 创建序列号生成器
//
// 使用示例:
//
//	gen, _ := idgen.NewSequencer(&idgen.SequenceConfig{
//	    KeyPrefix: "im:seq",
//	    Step: 1,
//	    TTL: 3600000000000, // 1 hour in nanoseconds
//	}, redisConn, idgen.WithLogger(logger))
//
//	// IM 场景使用
//	seq, _ := gen.Next(ctx, "alice")  // Alice 的消息序号
//	seq, _ := gen.Next(ctx, "bob")    // Bob 的消息序号
func NewSequencer(cfg *SequenceConfig, redis connector.RedisConnector, opts ...Option) (Sequencer, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrConfigNil, "sequence_config_nil")
	}
	if redis == nil {
		return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_nil")
	}
	if cfg.Step <= 0 {
		cfg.Step = 1
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger (添加 "sequencer" namespace)
	if opt.Logger != nil {
		opt.Logger = opt.Logger.With(clog.String("component", "sequencer"))
	}

	gen := &redisSequencer{
		redis:  redis,
		cfg:    cfg,
		logger: opt.Logger,
		meter:  opt.Meter,
	}

	return gen, nil
}

// buildKey 根据键名构建完整的 Redis 键
func (r *redisSequencer) buildKey(key string) string {
	if r.cfg.KeyPrefix == "" {
		return key
	}
	return fmt.Sprintf("%s:%s", r.cfg.KeyPrefix, key)
}

// Next 生成下一个序列号
func (r *redisSequencer) Next(ctx context.Context, key string) (int64, error) {
	redisKey := r.buildKey(key)
	client := r.redis.GetClient()

	// 使用 Redis INCRBY 命令生成序列号
	result, err := client.IncrBy(ctx, redisKey, r.cfg.Step).Result()
	if err != nil {
		if r.logger != nil {
			r.logger.Error("failed to increment sequence",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Int64("step", r.cfg.Step),
			)
		}
		return 0, xerrors.Wrap(err, "redis incrby failed")
	}

	// 检查是否需要循环
	if r.cfg.MaxValue > 0 && result > r.cfg.MaxValue {
		// 重置为步长
		resetValue := r.cfg.Step
		if resetValue > r.cfg.MaxValue {
			resetValue = r.cfg.Step
		}
		_, err := client.Set(ctx, redisKey, resetValue, 0).Result()
		if err != nil {
			if r.logger != nil {
				r.logger.Error("failed to reset sequence",
					clog.Error(err),
					clog.String("redis_key", redisKey),
					clog.String("key", key),
				)
			}
			return 0, xerrors.Wrap(err, "redis reset failed")
		}
		result = resetValue
	}

	// 设置 TTL
	if r.cfg.TTL > 0 {
		ttl := time.Duration(r.cfg.TTL)
		_, err = client.Expire(ctx, redisKey, ttl).Result()
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to set ttl",
					clog.Error(err),
					clog.String("redis_key", redisKey),
					clog.String("key", key),
					clog.Duration("ttl", ttl),
				)
			}
		}
	}

	if r.logger != nil {
		r.logger.Debug("generated sequence number",
			clog.String("redis_key", redisKey),
			clog.String("key", key),
			clog.Int64("seq", result),
		)
	}

	return result, nil
}

// NextBatch 批量生成序列号
func (r *redisSequencer) NextBatch(ctx context.Context, key string, count int) ([]int64, error) {
	if count <= 0 {
		return nil, xerrors.WithCode(ErrInvalidInput, "count_must_be_positive")
	}

	redisKey := r.buildKey(key)
	client := r.redis.GetClient()

	// 计算总增量
	totalIncrement := int64(count) * r.cfg.Step

	// 使用 Redis INCRBY 命令批量增加序列号
	endSeq, err := client.IncrBy(ctx, redisKey, totalIncrement).Result()
	if err != nil {
		if r.logger != nil {
			r.logger.Error("failed to batch increment sequence",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Int("count", count),
				clog.Int64("total_increment", totalIncrement),
			)
		}
		return nil, xerrors.Wrap(err, "redis incrby failed")
	}

	// 生成序列号数组
	seqs := make([]int64, count)
	for i := 0; i < count; i++ {
		seq := endSeq - int64(count-i-1)*r.cfg.Step

		// 检查最大值限制
		if r.cfg.MaxValue > 0 && seq > r.cfg.MaxValue {
			// 如果超出最大值，重置并重新开始
			resetValue := int64(count) * r.cfg.Step
			if resetValue > r.cfg.MaxValue {
				resetValue = r.cfg.Step
			}
			_, err := client.Set(ctx, redisKey, resetValue, 0).Result()
			if err != nil {
				if r.logger != nil {
					r.logger.Error("failed to reset sequence in batch",
						clog.Error(err),
						clog.String("redis_key", redisKey),
						clog.String("key", key),
					)
				}
				return nil, xerrors.Wrap(err, "redis reset failed")
			}
			for j := 0; j < count; j++ {
				seqs[j] = int64(j+1) * r.cfg.Step
				if seqs[j] > r.cfg.MaxValue {
					seqs[j] = r.cfg.Step
				}
			}
			break
		}

		seqs[i] = seq
	}

	// 设置 TTL
	if r.cfg.TTL > 0 {
		ttl := time.Duration(r.cfg.TTL)
		_, err = client.Expire(ctx, redisKey, ttl).Result()
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to set ttl for batch",
					clog.Error(err),
					clog.String("redis_key", redisKey),
					clog.String("key", key),
					clog.Duration("ttl", ttl),
				)
			}
		}
	}

	if r.logger != nil {
		r.logger.Debug("generated sequence batch",
			clog.String("redis_key", redisKey),
			clog.String("key", key),
			clog.Int("count", count),
			clog.Int64("start_seq", seqs[0]),
			clog.Int64("end_seq", seqs[len(seqs)-1]),
		)
	}

	return seqs, nil
}
