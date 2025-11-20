package idgen

import (
	"context"
	"fmt"

	"github.com/ceyewan/genesis/internal/idgen/allocator"
	"github.com/ceyewan/genesis/internal/idgen/snowflake"
	"github.com/ceyewan/genesis/internal/idgen/uuid"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/idgen/types"
)

// NewSnowflake 创建 Snowflake 生成器
func NewSnowflake(cfg *types.SnowflakeConfig, redisConn connector.RedisConnector, etcdConn connector.EtcdConnector) (types.Int64Generator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("snowflake config is nil")
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
		alloc = allocator.NewRedis(redisConn, cfg.KeyPrefix, cfg.TTL)
	case "etcd":
		if etcdConn == nil {
			return nil, fmt.Errorf("etcd connector is nil for method=etcd")
		}
		alloc = allocator.NewEtcd(etcdConn, cfg.KeyPrefix, cfg.TTL)
	default:
		return nil, fmt.Errorf("unsupported method: %s", cfg.Method)
	}

	// 创建生成器
	gen, err := snowflake.New(cfg, alloc)
	if err != nil {
		return nil, err
	}

	// 初始化 (分配 WorkerID 并启动保活)
	ctx := context.Background()
	if err := gen.Init(ctx); err != nil {
		return nil, fmt.Errorf("init snowflake failed: %w", err)
	}

	return gen, nil
}

// NewUUID 创建 UUID 生成器
func NewUUID(cfg *types.UUIDConfig) (types.Generator, error) {
	return uuid.New(cfg)
}
