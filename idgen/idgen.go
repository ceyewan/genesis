package idgen

import (
	"context"
	"fmt"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/internal/idgen/allocator"
)

// ========================================
// 接口定义 (Interface Definitions)
// ========================================

// Generator 通用 ID 生成器接口
type Generator interface {
	// String 返回字符串形式的 ID (UUID / Snowflake string)
	String() string
}

// Int64Generator 支持数字 ID 的生成器 (主要用于 Snowflake)
type Int64Generator interface {
	Generator
	// Int64 返回 int64 形式的 ID
	Int64() (int64, error)
}

// ========================================
// 配置定义 (Configuration)
// ========================================

// Config 是 ID 生成器的通用配置
type Config struct {
	// Mode 指定生成器模式: "snowflake" | "uuid" | "sequence"
	Mode string `yaml:"mode" json:"mode"`

	// Snowflake 雪花算法配置 (Mode="snowflake" 时必填)
	Snowflake *SnowflakeConfig `yaml:"snowflake" json:"snowflake"`

	// UUID UUID 配置 (Mode="uuid" 时必填)
	UUID *UUIDConfig `yaml:"uuid" json:"uuid"`

	// Sequence 序列号生成器配置 (Mode="sequence" 时必填)
	Sequence *SequenceConfig `yaml:"sequence" json:"sequence"`
}

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
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	gen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
//	    Method: "redis",
//	    DatacenterID: 1,
//	    KeyPrefix: "myapp:idgen:",
//	}, redisConn, nil, idgen.WithLogger(logger))
func NewSnowflake(cfg *SnowflakeConfig, redisConn connector.RedisConnector, etcdConn connector.EtcdConnector, opts ...Option) (Int64Generator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("snowflake config is nil")
	}

	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
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
		if redisConn == nil {
			return nil, fmt.Errorf("redis connector is nil for method=redis")
		}
		alloc = allocator.NewRedis(redisConn, cfg.KeyPrefix, cfg.TTL, opt.Logger)
	case "etcd":
		if etcdConn == nil {
			return nil, fmt.Errorf("etcd connector is nil for method=etcd")
		}
		alloc = allocator.NewEtcd(etcdConn, cfg.KeyPrefix, cfg.TTL, opt.Logger)
	default:
		return nil, fmt.Errorf("unsupported method: %s", cfg.Method)
	}

	// 创建生成器
	gen, err := newSnowflake(cfg, alloc, opt.Logger, opt.Meter, opt.Tracer)
	if err != nil {
		return nil, err
	}

	// 初始化 (分配 WorkerID 并启动保活) - 使用类型断言访问 Init 方法
	snowflakeGen, ok := gen.(*snowflakeGenerator)
	if !ok {
		return nil, fmt.Errorf("failed to cast to snowflakeGenerator")
	}

	ctx := context.Background()
	if err := snowflakeGen.Init(ctx); err != nil {
		return nil, fmt.Errorf("init snowflake failed: %w", err)
	}

	return gen, nil
}

// NewUUID 创建 UUID 生成器 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - cfg: UUID 配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	gen, _ := idgen.NewUUID(&idgen.UUIDConfig{
//	    Version: "v7",
//	}, idgen.WithLogger(logger))
func NewUUID(cfg *UUIDConfig, opts ...Option) (Generator, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger (添加 "idgen" namespace)
	if opt.Logger != nil {
		opt.Logger = opt.Logger.With(clog.String("component", "idgen"))
	}

	return newUUID(cfg, opt.Logger, opt.Meter, opt.Tracer)
}
