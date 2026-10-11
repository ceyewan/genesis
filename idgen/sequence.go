package idgen

import (
	"context"
	"fmt"
	"math"
	"strconv"

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

	seq, err := r.increment(ctx, client, redisKey, r.cfg.Step)
	if err != nil {
		return 0, err
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

	if int64(count) > math.MaxInt64/r.cfg.Step {
		return nil, xerrors.WithCode(ErrSequenceExhausted, "batch_increment_overflow")
	}
	endSeq, err := r.increment(ctx, client, redisKey, r.cfg.Step*int64(count))
	if err != nil {
		return nil, err
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

// increment 将整数计算交给 Redis，Lua 只比较十进制字符串，避免 double 精度损失。
func (r *redisSequencer) increment(ctx context.Context, client *redis.Client, key string, delta int64) (int64, error) {
	maximum := r.cfg.MaxValue
	if maximum == 0 {
		maximum = math.MaxInt64
	}
	if delta > maximum {
		return 0, ErrSequenceExhausted
	}
	const script = `
		local current = redis.call("GET", KEYS[1]) or "0"
		local limit = ARGV[2]
		if #current > #limit or (#current == #limit and current > limit) then
			return false
		end
		redis.call("INCRBY", KEYS[1], ARGV[1])
		if tonumber(ARGV[3]) > 0 then
			redis.call("PEXPIRE", KEYS[1], ARGV[3])
		end
		return redis.call("GET", KEYS[1])
	`
	value, err := client.Eval(ctx, script, []string{key}, delta, maximum-delta, r.cfg.TTL.Milliseconds()).Text()
	if xerrors.Is(err, redis.Nil) {
		return 0, ErrSequenceExhausted
	}
	if err != nil {
		return 0, xerrors.Wrap(err, "increment sequence")
	}
	result, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, xerrors.Wrap(err, "parse sequence result")
	}
	return result, nil
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
