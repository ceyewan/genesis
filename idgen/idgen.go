// Package idgen 提供高性能的分布式 ID 生成能力，支持多种 ID 生成策略：
//
//   - Snowflake: 基于雪花算法的分布式有序 ID 生成，支持多种 WorkerID 分配策略
//   - UUID: 标准 UUID 生成，支持 v4 和 v7 版本
//   - Sequence: 基于 Redis 的分布式序列号生成器
//
// 设计原则:
//   - 简单易用: 提供简洁的工厂函数和配置接口
//   - 高性能: 优化热路径，支持批量生成
//   - 可观测: 内置指标收集和结构化日志
//   - 容错性: 优雅处理网络异常和时钟回拨
//
// 基本使用:
//
//	snowflakeGen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
//	    Method: "redis",
//	    DatacenterID: 1,
//	}, redisConn, nil)
//
//	uuidGen, _ := idgen.NewUUID(&idgen.UUIDConfig{
//	    Version: "v7",
//	})
//
//	sequencer, _ := idgen.NewSequencer(&idgen.SequenceConfig{
//	    KeyPrefix: "im:seq",
//	    Step: 1,
//	}, redisConn)
package idgen

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idgen/internal/allocator"
	"github.com/ceyewan/genesis/xerrors"
)

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Generator 通用 ID 生成器接口
type Generator interface {
	// Next 返回字符串形式的 ID (UUID / Snowflake string)
	Next() string
}

// Int64Generator 支持数字 ID 的生成器 (主要用于 Snowflake)
type Int64Generator interface {
	Generator
	// NextInt64 返回 int64 形式的 ID
	NextInt64() (int64, error)
}

// Sequencer 序列号生成器接口
// 提供基于 Redis 的分布式序列号生成能力
type Sequencer interface {
	// Next 为指定键生成下一个序列号
	Next(ctx context.Context, key string) (int64, error)

	// NextBatch 为指定键批量生成序列号
	NextBatch(ctx context.Context, key string, count int) ([]int64, error)
}

// ========================================
// 工厂函数 (Factory Functions)
// ========================================

// NewSnowflake 创建 Snowflake 生成器 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - cfg: Snowflake 配置
//   - redisConn: Redis 连接器 (method="redis" 时必需)
//   - etcdConn: Etcd 连接器 (method="etcd" 时必需)
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	gen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
//	    Method: "redis",
//	    DatacenterID: 1,
//	    KeyPrefix: "myapp:idgen:",
//	}, redisConn, nil, idgen.WithLogger(logger))
func NewSnowflake(cfg *SnowflakeConfig, redis connector.RedisConnector, etcd connector.EtcdConnector, opts ...Option) (Int64Generator, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrConfigNil, "snowflake_config_nil")
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger (添加 "idgen" namespace)
	if opt.Logger != nil {
		opt.Logger = opt.Logger.With(clog.String("component", "idgen"))
	}

	// 根据 method 选择分配器
	var alloc allocator.Allocator
	switch cfg.Method {
	case "static":
		alloc = allocator.NewStatic(cfg.WorkerID)
	case "ip_24":
		alloc = allocator.NewIP()
	case "redis":
		if redis == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "redis_connector_nil")
		}
		alloc = allocator.NewRedis(redis, cfg.KeyPrefix, cfg.TTL, opt.Logger)
	case "etcd":
		if etcd == nil {
			return nil, xerrors.WithCode(ErrConnectorNil, "etcd_connector_nil")
		}
		alloc = allocator.NewEtcd(etcd, cfg.KeyPrefix, cfg.TTL, opt.Logger)
	default:
		return nil, xerrors.Wrapf(ErrInvalidMethod, "method: %s", cfg.Method)
	}

	// 创建生成器
	gen, err := newSnowflakeGen(cfg, alloc, opt.Logger, opt.Meter)
	if err != nil {
		return nil, err
	}

	// 初始化 (分配 WorkerID 并启动保活)
	s, ok := gen.(*snowflakeGen)
	if !ok {
		return nil, xerrors.New("failed to cast to snowflakeGen")
	}

	ctx := context.Background()
	if err := s.init(ctx); err != nil {
		return nil, xerrors.Wrap(err, "init snowflake")
	}

	return gen, nil
}

// NewUUID 创建 UUID 生成器 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - cfg: UUID 配置
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	gen, _ := idgen.NewUUID(&idgen.UUIDConfig{
//	    Version: "v7",
//	}, idgen.WithLogger(logger))
func NewUUID(cfg *UUIDConfig, opts ...Option) (Generator, error) {
	if cfg == nil {
		return nil, xerrors.WithCode(ErrConfigNil, "uuid_config_nil")
	}

	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger (添加 "idgen" namespace)
	if opt.Logger != nil {
		opt.Logger = opt.Logger.With(clog.String("component", "idgen"))
	}

	return newUUIDGen(cfg, opt.Logger, opt.Meter)
}

// ========================================
// 配置定义 (Configuration)
// ========================================

// SnowflakeConfig 雪花算法配置
type SnowflakeConfig struct {
	// Method 指定 WorkerID 的获取方式
	// 可选: "static" | "ip_24" | "redis" | "etcd"
	Method string `yaml:"method" json:"method"`

	// WorkerID 当 Method="static" 时手动指定
	WorkerID int64 `yaml:"worker_id" json:"worker_id"`

	// DatacenterID 数据中心 ID (可选，默认 0)
	DatacenterID int64 `yaml:"datacenter_id" json:"datacenter_id"`

	// KeyPrefix Redis/Etcd 键前缀 (可选，默认 "genesis:idgen:worker")
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// TTL 租约 TTL 秒数 (可选，默认 30)
	TTL int `yaml:"ttl" json:"ttl"`

	// MaxDriftMs 允许的最大时钟回拨毫秒数 (可选，默认 5ms)
	MaxDriftMs int64 `yaml:"max_drift_ms" json:"max_drift_ms"`

	// MaxWaitMs 时钟回拨时最大等待毫秒数 (可选，默认 1000ms)
	// 超过此值则直接熔断
	MaxWaitMs int64 `yaml:"max_wait_ms" json:"max_wait_ms"`
}

// UUIDConfig UUID 配置
type UUIDConfig struct {
	// Version UUID 版本 (可选，默认 "v4")
	// 支持: "v4" | "v7"
	Version string `yaml:"version" json:"version"`
}

// SequenceConfig 序列号生成器配置
type SequenceConfig struct {
	// KeyPrefix 键前缀
	KeyPrefix string `yaml:"key_prefix" json:"key_prefix"`

	// Step 步长，默认为 1
	Step int64 `yaml:"step" json:"step"`

	// MaxValue 最大值限制，达到后循环（0 表示不限制）
	MaxValue int64 `yaml:"max_value" json:"max_value"`

	// TTL Redis 键过期时间，0 表示永不过期
	TTL int64 `yaml:"ttl" json:"ttl"`
}