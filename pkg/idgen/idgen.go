package idgen

import (
	"github.com/ceyewan/genesis/internal/idgen"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/idgen/types"
)

// Generator 接口别名，方便使用
type Generator = types.Generator

// Int64Generator 接口别名
type Int64Generator = types.Int64Generator

// Config 配置别名
type Config = types.Config

// NewSnowflake 创建 Snowflake 生成器
func NewSnowflake(cfg *types.SnowflakeConfig, redisConn connector.RedisConnector, etcdConn connector.EtcdConnector) (Int64Generator, error) {
	return idgen.NewSnowflake(cfg, redisConn, etcdConn)
}

// NewUUID 创建 UUID 生成器
func NewUUID(cfg *types.UUIDConfig) (Generator, error) {
	return idgen.NewUUID(cfg)
}
