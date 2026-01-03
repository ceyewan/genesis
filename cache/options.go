package cache

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
)

// Option 缓存组件选项函数
type Option func(*options)

// options 选项结构（内部使用）
type options struct {
	Logger    clog.Logger
	Meter     metrics.Meter
	RedisConn connector.RedisConnector
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 Namespace: logger.WithNamespace("cache")
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.Logger = l.WithNamespace("cache")
		}
	}
}

// WithMeter 注入指标 Meter
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.Meter = m
	}
}

// WithRedisConnector 注入 Redis 连接器 (仅用于分布式模式)
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		o.RedisConn = conn
	}
}
