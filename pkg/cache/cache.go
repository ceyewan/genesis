package cache

import (
	"github.com/ceyewan/genesis/internal/cache"
	"github.com/ceyewan/genesis/pkg/cache/types"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
)

// Cache 接口别名
type Cache = types.Cache

// Config 配置别名
type Config = types.Config

// NewRedis 创建 Redis 缓存
func NewRedis(conn connector.RedisConnector, cfg *Config, logger clog.Logger) (Cache, error) {
	return cache.NewRedis(conn, cfg, logger)
}
