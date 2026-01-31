package idgen

import (
	"context"
	"fmt"

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
	redis      connector.RedisConnector
	cfg        *SequencerConfig
	logger     clog.Logger
	seqCounter metrics.Counter
}

// NewSequencer 创建序列号生成器（配置驱动，目前仅支持 Redis）
//
// 使用示例:
//
//	seq, _ := idgen.NewSequencer(&idgen.SequencerConfig{
//	    KeyPrefix: "im:seq",
//	    Step:      1,
//	    TTL:       3600, // 1 hour (秒)
//	}, idgen.WithRedisConnector(redisConn))
//
//	// IM 场景使用
//	id, _ := seq.Next(ctx, "alice")  // Alice 的消息序号
//	id, _ := seq.Next(ctx, "bob")    // Bob 的消息序号
func NewSequencer(cfg *SequencerConfig, opts ...Option) (Sequencer, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrInvalidInput, "config_nil")
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	meter := opt.Meter
	if meter == nil {
		meter = metrics.Discard()
	}
	seqCounter, _ := meter.Counter(MetricSequenceGenerated, "序列号生成总数")

	// 目前仅支持 Redis
	switch cfg.Driver {
	case "redis":
		if opt.RedisConnector == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
		}
		return newRedisSequencer(cfg, opt.RedisConnector, opt.Logger, seqCounter)
	default:
		return nil, xerrors.WithCode(ErrInvalidInput, "unsupported_driver")
	}
}

func newRedisSequencer(cfg *SequencerConfig, redis connector.RedisConnector, logger clog.Logger, seqCounter metrics.Counter) (Sequencer, error) {
	if logger != nil {
		logger = logger.With(clog.String("component", "sequencer"))
	}

	return &redisSequencer{
		redis:      redis,
		cfg:        cfg,
		logger:     logger,
		seqCounter: seqCounter,
	}, nil
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

	// Lua 脚本：原子执行 IncrBy + MaxValue Check + Reset + Expire
	script := `
		local key = KEYS[1]
		local step = tonumber(ARGV[1])
		local max = tonumber(ARGV[2])
		local ttl = tonumber(ARGV[3])

		local v = redis.call("INCRBY", key, step)
		local current = tonumber(v)

		if max > 0 and current > max then
			-- 超过最大值，重置为步长
			redis.call("SET", key, step)
			current = step
		end

		if ttl > 0 then
			redis.call("EXPIRE", key, ttl)
		end

		return current
	`

	result, err := client.Eval(ctx, script, []string{redisKey}, r.cfg.Step, r.cfg.MaxValue, r.cfg.TTL).Result()
	if err != nil {
		if r.logger != nil {
			r.logger.Error("failed to generate sequence",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
			)
		}
		return 0, xerrors.Wrap(err, "redis_eval_failed")
	}

	seq, ok := result.(int64)
	if !ok {
		return 0, xerrors.New("unexpected result type from redis")
	}

	if r.logger != nil {
		r.logger.Debug("generated sequence number",
			clog.String("redis_key", redisKey),
			clog.String("key", key),
			clog.Int64("seq", seq),
		)
	}

	r.seqCounter.Inc(ctx)

	return seq, nil
}

// NextBatch 批量生成序列号
func (r *redisSequencer) NextBatch(ctx context.Context, key string, count int) ([]int64, error) {
	if count <= 0 {
		return nil, xerrors.WithCode(ErrInvalidInput, "count_must_be_positive")
	}

	redisKey := r.buildKey(key)
	client := r.redis.GetClient()

	// Lua 脚本：原子执行 Batch IncrBy + MaxValue Check + Reset + Expire
	script := `
		local key = KEYS[1]
		local step = tonumber(ARGV[1])
		local count = tonumber(ARGV[2])
		local max = tonumber(ARGV[3])
		local ttl = tonumber(ARGV[4])

		local total_inc = step * count
		local v = redis.call("INCRBY", key, total_inc)
		local end_seq = tonumber(v)

		if max > 0 and end_seq > max then
			-- 超过最大值，重置为 total_inc (相当于从 0 开始重新计数)
			redis.call("SET", key, total_inc)
			end_seq = total_inc
		end

		if ttl > 0 then
			redis.call("EXPIRE", key, ttl)
		end

		return end_seq
	`

	result, err := client.Eval(ctx, script, []string{redisKey}, r.cfg.Step, count, r.cfg.MaxValue, r.cfg.TTL).Result()
	if err != nil {
		if r.logger != nil {
			r.logger.Error("failed to batch generate sequence",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
			)
		}
		return nil, xerrors.Wrap(err, "redis_eval_failed")
	}

	endSeq, ok := result.(int64)
	if !ok {
		return nil, xerrors.New("unexpected result type from redis")
	}

	// 生成序列号数组
	seqs := make([]int64, count)
	for i := 0; i < count; i++ {
		// 倒推每个序列号
		seqs[i] = endSeq - int64(count-i-1)*r.cfg.Step
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

	r.seqCounter.Add(ctx, float64(count))

	return seqs, nil
}

// Set 直接设置序列号的值
func (r *redisSequencer) Set(ctx context.Context, key string, value int64) error {
	if value < 0 {
		return xerrors.WithCode(ErrInvalidInput, "negative_value")
	}

	redisKey := r.buildKey(key)
	client := r.redis.GetClient()

	if err := client.Set(ctx, redisKey, value, 0).Err(); err != nil {
		if r.logger != nil {
			r.logger.Error("failed to set sequence value",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Int64("value", value),
			)
		}
		return xerrors.Wrap(err, "redis_set_failed")
	}

	if r.logger != nil {
		r.logger.Debug("set sequence value",
			clog.String("redis_key", redisKey),
			clog.String("key", key),
			clog.Int64("value", value),
		)
	}

	return nil
}

// SetIfNotExists 仅当键不存在时设置序列号的值
func (r *redisSequencer) SetIfNotExists(ctx context.Context, key string, value int64) (bool, error) {
	if value < 0 {
		return false, xerrors.WithCode(ErrInvalidInput, "negative_value")
	}

	redisKey := r.buildKey(key)
	client := r.redis.GetClient()

	// 使用 SETNX (Set if Not eXists) 命令
	result, err := client.SetNX(ctx, redisKey, value, 0).Result()
	if err != nil {
		if r.logger != nil {
			r.logger.Error("failed to set sequence if not exists",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Int64("value", value),
			)
		}
		return false, xerrors.Wrap(err, "redis_setnx_failed")
	}

	if r.logger != nil {
		if result {
			r.logger.Debug("set sequence value (new key)",
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Int64("value", value),
			)
		} else {
			r.logger.Debug("sequence key already exists",
				clog.String("redis_key", redisKey),
				clog.String("key", key),
			)
		}
	}

	return result, nil
}
