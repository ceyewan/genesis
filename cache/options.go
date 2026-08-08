package cache

import (
	"github.com/ceyewan/genesis/cache/serializer"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// Option 缓存组件选项函数。
type Option func(*options)

type options struct {
	Logger     clog.Logger
	RedisConn  connector.RedisConnector
	Serializer serializer.Serializer
}

// WithSerializer injects a custom serializer. It takes precedence over the
// Serializer name in LocalConfig or DistributedConfig.
func WithSerializer(s serializer.Serializer) Option {
	return func(o *options) {
		if s != nil {
			o.Serializer = s
		}
	}
}

// WithLogger 注入日志记录器。
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.Logger = l.WithNamespace("cache")
		}
	}
}

// WithRedisConnector 注入 Redis 连接器。
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.RedisConn = conn
		}
	}
}
