package idgen

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
)

// ========================================
// 序列号生成器接口 (Sequence Generator Interface)
// ========================================

// SequenceGenerator 序列号生成器接口
// 提供基于 Redis 的分布式序列号生成能力
type SequenceGenerator interface {
	// Next 为指定键生成下一个序列号
	Next(ctx context.Context, key string) (int64, error)

	// NextBatch 为指定键批量生成序列号
	NextBatch(ctx context.Context, key string, count int) ([]int64, error)
}

// ========================================
// 配置定义 (Configuration)
// ========================================

// SequenceConfig 序列号生成器配置
type SequenceConfig struct {
	// KeyPrefix 键前缀
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// Step 步长，默认为 1
	Step int64 `yaml:"step" json:"step"`

	// MaxValue 最大值限制，达到后循环（0 表示不限制）
	MaxValue int64 `yaml:"max_value" json:"max_value"`

	// TTL Redis 键过期时间，0 表示永不过期
	TTL time.Duration `yaml:"ttl" json:"ttl"`
}

// ========================================
// Redis 实现 (Redis Implementation)
// ========================================

// redisSequenceGenerator Redis 实现的序列号生成器
type redisSequenceGenerator struct {
	redisConn connector.RedisConnector
	config    *SequenceConfig
	logger    clog.Logger
}

// NewSequence 创建序列号生成器
//
// 使用示例:
//
//	gen, _ := idgen.NewSequence(&idgen.SequenceConfig{
//		KeyPrefix: "im:seq",
//		Step:      1,
//		TTL:       time.Hour,
//	}, redisConn, idgen.WithLogger(logger))
//
//	// IM 场景使用
//	seq, _ := gen.Next(ctx, "alice")  // Alice 的消息序号
//	seq, _ := gen.Next(ctx, "bob")    // Bob 的消息序号
func NewSequence(cfg *SequenceConfig, redisConn connector.RedisConnector, opts ...Option) (SequenceGenerator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("sequence config is nil")
	}
	if redisConn == nil {
		return nil, fmt.Errorf("redis connector is nil")
	}
	if cfg.Step <= 0 {
		cfg.Step = 1 // 默认步长为 1
	}

	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger (添加 "sequence" namespace)
	if opt.Logger != nil {
		opt.Logger = opt.Logger.With(clog.String("component", "sequence"))
	}

	gen := &redisSequenceGenerator{
		redisConn: redisConn,
		config:    cfg,
		logger:    opt.Logger,
	}

	return gen, nil
}

// buildKey 根据键名构建完整的 Redis 键
func (g *redisSequenceGenerator) buildKey(key string) string {
	if g.config.KeyPrefix == "" {
		return key
	}
	return fmt.Sprintf("%s:%s", g.config.KeyPrefix, key)
}

// Next 生成下一个序列号
func (g *redisSequenceGenerator) Next(ctx context.Context, key string) (int64, error) {
	redisKey := g.buildKey(key)
	client := g.redisConn.GetClient()

	// 使用 Redis INCRBY 命令生成序列号
	result, err := client.IncrBy(ctx, redisKey, g.config.Step).Result()
	if err != nil {
		g.logger.Error("failed to increment sequence",
			clog.Error(err),
			clog.String("redis_key", redisKey),
			clog.String("key", key),
			clog.Int64("step", g.config.Step),
		)
		return 0, fmt.Errorf("redis incrby failed: %w", err)
	}

	// 检查是否需要循环
	if g.config.MaxValue > 0 && result > g.config.MaxValue {
		// 重置为步长（Redis INCR 从 1 开始，所以这里是步长值）
		resetValue := g.config.Step
		if resetValue > g.config.MaxValue {
			resetValue = g.config.Step // 如果步长就超过最大值，仍然从步长开始
		}
		_, err := client.Set(ctx, redisKey, resetValue, 0).Result()
		if err != nil {
			g.logger.Error("failed to reset sequence",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
			)
			return 0, fmt.Errorf("redis reset failed: %w", err)
		}
		result = resetValue
	}

	// 设置 TTL
	if g.config.TTL > 0 {
		_, err = client.Expire(ctx, redisKey, g.config.TTL).Result()
		if err != nil {
			g.logger.Warn("failed to set ttl",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Duration("ttl", g.config.TTL),
			)
		}
	}

	g.logger.Debug("generated sequence number",
		clog.String("redis_key", redisKey),
		clog.String("key", key),
		clog.Int64("seq", result),
	)

	return result, nil
}

// NextBatch 批量生成序列号
func (g *redisSequenceGenerator) NextBatch(ctx context.Context, key string, count int) ([]int64, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive")
	}

	redisKey := g.buildKey(key)
	client := g.redisConn.GetClient()

	// 计算总增量
	totalIncrement := int64(count) * g.config.Step

	// 使用 Redis INCRBY 命令批量增加序列号
	endSeq, err := client.IncrBy(ctx, redisKey, totalIncrement).Result()
	if err != nil {
		g.logger.Error("failed to batch increment sequence",
			clog.Error(err),
			clog.String("redis_key", redisKey),
			clog.String("key", key),
			clog.Int("count", count),
			clog.Int64("total_increment", totalIncrement),
		)
		return nil, fmt.Errorf("redis incrby failed: %w", err)
	}

	// 生成序列号数组
	seqs := make([]int64, count)
	for i := 0; i < count; i++ {
		seq := endSeq - int64(count-i-1)*g.config.Step

		// 检查最大值限制
		if g.config.MaxValue > 0 && seq > g.config.MaxValue {
			// 如果超出最大值，重置并重新开始
			resetValue := int64(count) * g.config.Step
			if resetValue > g.config.MaxValue {
				resetValue = g.config.Step
			}
			_, err := client.Set(ctx, redisKey, resetValue, 0).Result()
			if err != nil {
				g.logger.Error("failed to reset sequence in batch",
					clog.Error(err),
					clog.String("redis_key", redisKey),
					clog.String("key", key),
				)
				return nil, fmt.Errorf("redis reset failed: %w", err)
			}
			for j := 0; j < count; j++ {
				seqs[j] = int64(j+1) * g.config.Step
				if seqs[j] > g.config.MaxValue {
					seqs[j] = g.config.Step // 如果还是超出，从步长开始
				}
			}
			break
		}

		seqs[i] = seq
	}

	// 设置 TTL
	if g.config.TTL > 0 {
		_, err = client.Expire(ctx, redisKey, g.config.TTL).Result()
		if err != nil {
			g.logger.Warn("failed to set ttl for batch",
				clog.Error(err),
				clog.String("redis_key", redisKey),
				clog.String("key", key),
				clog.Duration("ttl", g.config.TTL),
			)
		}
	}

	g.logger.Debug("generated sequence batch",
		clog.String("redis_key", redisKey),
		clog.String("key", key),
		clog.Int("count", count),
		clog.Int64("start_seq", seqs[0]),
		clog.Int64("end_seq", seqs[len(seqs)-1]),
	)

	return seqs, nil
}

// ========================================
// 便捷函数 (Convenience Functions)
// ========================================

// MustNewSequence 创建序列号生成器，出错时 panic
// 仅用于初始化阶段，避免使用
func MustNewSequence(cfg *SequenceConfig, redisConn connector.RedisConnector, opts ...Option) SequenceGenerator {
	gen, err := NewSequence(cfg, redisConn, opts...)
	if err != nil {
		panic(err)
	}
	return gen
}