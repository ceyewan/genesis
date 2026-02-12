package ratelimit

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	metrics "github.com/ceyewan/genesis/metrics"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger    clog.Logger
	meter     metrics.Meter
	redisConn connector.RedisConnector
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithMeter 设置 Meter
func WithMeter(meter metrics.Meter) Option {
	return func(o *options) {
		o.meter = meter
	}
}

// WithRedisConnector 设置 Redis 连接器（用于分布式限流）
func WithRedisConnector(redisConn connector.RedisConnector) Option {
	return func(o *options) {
		o.redisConn = redisConn
	}
}
