package cache

import (
	"github.com/ceyewan/genesis/internal/cache/redis"
	"github.com/ceyewan/genesis/pkg/cache/types"
	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
)

// Cache 接口别名
type Cache = types.Cache

// Config 配置别名
type Config = types.Config

// New 创建缓存实例 (独立模式)
// 这是标准的工厂函数，支持在不依赖 Container 的情况下独立实例化
//
// 参数:
//   - conn: Redis 连接器
//   - cfg: 缓存配置
//   - opts: 可选参数 (Logger, Meter, Tracer)
//
// 使用示例:
//
//	redisConn, _ := connector.NewRedis(redisConfig)
//	cache, _ := cache.New(redisConn, &cache.Config{
//	    Prefix: "myapp:",
//	    Serializer: "json",
//	}, cache.WithLogger(logger))
func New(conn connector.RedisConnector, cfg *Config, opts ...Option) (Cache, error) {
	// 应用选项
	opt := Options{
		Logger: clog.Default(), // 默认 Logger
	}
	for _, o := range opts {
		o(&opt)
	}

	return redis.New(conn, cfg, opt.Logger, opt.Meter, opt.Tracer)
}
