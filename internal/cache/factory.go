package cache

import (
	"github.com/ceyewan/genesis/internal/cache/redis"
	"github.com/ceyewan/genesis/pkg/cache/types"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
)

// NewRedis 创建 Redis 缓存
func NewRedis(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger) (types.Cache, error) {
	return redis.New(conn, cfg, logger)
}
