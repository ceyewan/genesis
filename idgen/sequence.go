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
		return nil, xerrors.WithCode(ErrInvalidInput, "sequence_config_nil")
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

	// TTL 单位为秒
	ttlSec := int(r.cfg.TTL / 1e9) // 纳秒转秒
	if r.cfg.TTL > 0 && ttlSec == 0 {
		ttlSec = 1 // 至少 1 秒
	}

	result, err := client.Eval(ctx, script, []string{redisKey}, r.cfg.Step, r.cfg.MaxValue, ttlSec).Result()
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

	// TTL 单位为秒
	ttlSec := int(r.cfg.TTL / 1e9)
	if r.cfg.TTL > 0 && ttlSec == 0 {
		ttlSec = 1
	}

	result, err := client.Eval(ctx, script, []string{redisKey}, r.cfg.Step, count, r.cfg.MaxValue, ttlSec).Result()
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

	return seqs, nil
}
