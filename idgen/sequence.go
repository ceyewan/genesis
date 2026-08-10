package idgen

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"

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
//	    TTL:       time.Hour,
//	}, idgen.WithRedisConnector(redisConn))
//
//	// IM 场景使用
//	id, _ := seq.Next(ctx, "alice")  // Alice 的消息序号
//	id, _ := seq.Next(ctx, "bob")    // Bob 的消息序号
func NewSequencer(cfg *SequencerConfig, opts ...Option) (Sequencer, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrInvalidInput, "config_nil")
	}

	config := *cfg
	config.setDefaults()
	if err := config.validate(); err != nil {
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
	switch config.Driver {
	case "redis":
		if opt.RedisConnector == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
		}
		if opt.RedisConnector.GetClient() == nil {
			return nil, xerrors.Wrap(connector.ErrClientNil, "idgen: redis connector is not connected")
		}
		return newRedisSequencer(&config, opt.RedisConnector, opt.Logger, seqCounter)
	default:
		return nil, xerrors.WithCode(ErrInvalidInput, "unsupported_driver")
	}
}

func newRedisSequencer(cfg *SequencerConfig, redis connector.RedisConnector, logger clog.Logger, seqCounter metrics.Counter) (Sequencer, error) {
	if logger == nil {
		logger = clog.Discard()
	}

	return &redisSequencer{
		redis:      redis,
		cfg:        cfg,
		logger:     logger.With(clog.String("component", "sequencer")),
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

func (r *redisSequencer) client() (*redis.Client, error) {
	if r.redis == nil {
		return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_required")
	}
	client := r.redis.GetClient()
	if client == nil {
		return nil, xerrors.Wrap(connector.ErrClientNil, "idgen: redis connector is not connected")
	}
	return client, nil
}

// Next 生成下一个序列号
func (r *redisSequencer) Next(ctx context.Context, key string) (int64, error) {
	redisKey := r.buildKey(key)
	client, err := r.client()
	if err != nil {
		return 0, err
	}

	// Lua 脚本：原子执行 IncrBy + MaxValue Check + rollback + Expire。
	// 达到上限时回滚本次增量，绝不重置或回绕。
	script := `
		local key = KEYS[1]
		local step = tonumber(ARGV[1])
		local max = tonumber(ARGV[2])
		local ttl_ms = tonumber(ARGV[3])

		local v = redis.call("INCRBY", key, step)
		local current = tonumber(v)

		if max > 0 and current > max then
			redis.call("DECRBY", key, step)
			return {0, current}
		end

		if ttl_ms > 0 then
			redis.call("PEXPIRE", key, ttl_ms)
		end

		return {1, current}
	`

	result, err := client.Eval(ctx, script, []string{redisKey}, r.cfg.Step, r.cfg.MaxValue, r.cfg.TTL.Milliseconds()).Slice()
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

	if len(result) != 2 {
		return 0, xerrors.New("unexpected result type from redis")
	}
	ok, okType := result[0].(int64)
	seq, seqType := result[1].(int64)
	if !okType || !seqType {
		return 0, xerrors.New("unexpected result type from redis")
	}
	if ok != 1 {
		return 0, xerrors.WithCode(ErrSequenceExhausted, "max_value_exceeded")
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
	client, err := r.client()
	if err != nil {
		return nil, err
	}

	// Lua 脚本：原子执行 Batch IncrBy + MaxValue Check + rollback + Expire。
	script := `
		local key = KEYS[1]
		local step = tonumber(ARGV[1])
		local count = tonumber(ARGV[2])
		local max = tonumber(ARGV[3])
		local ttl_ms = tonumber(ARGV[4])

		local total_inc = step * count
		local v = redis.call("INCRBY", key, total_inc)
		local end_seq = tonumber(v)

		if max > 0 and end_seq > max then
			redis.call("DECRBY", key, total_inc)
			return {0, end_seq}
		end

		if ttl_ms > 0 then
			redis.call("PEXPIRE", key, ttl_ms)
		end

		return {1, end_seq}
	`

	result, err := client.Eval(ctx, script, []string{redisKey}, r.cfg.Step, count, r.cfg.MaxValue, r.cfg.TTL.Milliseconds()).Slice()
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

	if len(result) != 2 {
		return nil, xerrors.New("unexpected result type from redis")
	}
	ok, okType := result[0].(int64)
	endSeq, seqType := result[1].(int64)
	if !okType || !seqType {
		return nil, xerrors.New("unexpected result type from redis")
	}
	if ok != 1 {
		return nil, xerrors.WithCode(ErrSequenceExhausted, "max_value_exceeded")
	}

	// 生成序列号数组
	seqs := make([]int64, count)
	for i := range count {
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
	if r.cfg.MaxValue > 0 && value > r.cfg.MaxValue {
		return xerrors.WithCode(ErrSequenceExhausted, "value_exceeds_max")
	}

	redisKey := r.buildKey(key)
	client, err := r.client()
	if err != nil {
		return err
	}
	expiration := r.cfg.TTL

	if err := client.Set(ctx, redisKey, value, expiration).Err(); err != nil {
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
	if r.cfg.MaxValue > 0 && value > r.cfg.MaxValue {
		return false, xerrors.WithCode(ErrSequenceExhausted, "value_exceeds_max")
	}

	redisKey := r.buildKey(key)
	client, err := r.client()
	if err != nil {
		return false, err
	}
	expiration := r.cfg.TTL

	// 使用 SETNX (Set if Not eXists) 命令
	result, err := client.SetNX(ctx, redisKey, value, expiration).Result()
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
