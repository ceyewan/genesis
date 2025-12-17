package dlock

import (
	"fmt"

	"github.com/ceyewan/genesis/internal/dlock/etcd"
	"github.com/ceyewan/genesis/internal/dlock/redis"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/dlock/types"
	"github.com/ceyewan/genesis/pkg/metrics"
)

// NewRedis 创建 Redis 分布式锁
func NewRedis(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger, meter metrics.Meter) (types.Locker, error) {
	// 使用默认 Logger 如果未提供
	if logger == nil {
		logger = clog.Default()
	}

	return redis.New(conn, cfg, logger, meter)
}

// NewEtcd 创建 Etcd 分布式锁
func NewEtcd(conn connector.EtcdConnector, cfg *types.Config, logger clog.Logger, meter metrics.Meter) (types.Locker, error) {
	// 使用默认 Logger 如果未提供
	if logger == nil {
		logger = clog.Default()
	}

	return etcd.New(conn, cfg, logger, meter)
}

// New 根据配置创建分布式锁
// 注意：这里需要传入具体的 Connector，但由于 Connector 类型不同，
// 通常建议直接使用 NewRedis 或 NewEtcd。
// 此函数仅作为示例或特定场景使用，可能需要 interface{} 类型的 conn。
func New(conn interface{}, cfg *types.Config, logger clog.Logger, meter metrics.Meter) (types.Locker, error) {
	switch cfg.Backend {
	case types.BackendRedis:
		if c, ok := conn.(connector.RedisConnector); ok {
			return NewRedis(c, cfg, logger, meter)
		}
		return nil, fmt.Errorf("invalid connector type for redis backend")
	case types.BackendEtcd:
		if c, ok := conn.(connector.EtcdConnector); ok {
			return NewEtcd(c, cfg, logger, meter)
		}
		return nil, fmt.Errorf("invalid connector type for etcd backend")
	default:
		return nil, fmt.Errorf("unsupported backend: %s", cfg.Backend)
	}
}
