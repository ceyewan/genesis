package idgen

import (
	"github.com/ceyewan/genesis/internal/idgen"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/idgen/types"
)

// ========================================
// 类型导出 (Type Exports)
// ========================================

// Generator 接口别名，方便使用
type Generator = types.Generator

// Int64Generator 接口别名
type Int64Generator = types.Int64Generator

// Config 配置别名
type Config = types.Config

// SnowflakeConfig 配置别名
type SnowflakeConfig = types.SnowflakeConfig

// UUIDConfig 配置别名
type UUIDConfig = types.UUIDConfig

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
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return idgen.NewSnowflake(cfg, redisConn, etcdConn, opt.Logger, opt.Meter, opt.Tracer)
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

	return idgen.NewUUID(cfg, opt.Logger, opt.Meter, opt.Tracer)
}
