package mq

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
)

// Option MQ 组件选项函数
type Option func(*options)

// options 选项结构（内部使用）
type options struct {
	Logger         clog.Logger
	Meter          metrics.Meter
	RedisConnector connector.RedisConnector
	NATSConnector  connector.NATSConnector
}

// WithLogger 注入日志记录器
// 组件内部会自动追加 Namespace: logger.WithNamespace("mq")
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.Logger = l.WithNamespace("mq")
		}
	}
}

// WithMeter 注入指标 Meter（默认使用 metrics.Discard）
func WithMeter(m metrics.Meter) Option {
	return func(o *options) {
		o.Meter = m
	}
}

// WithRedisConnector 注入 Redis 连接器
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.RedisConnector = conn
		}
	}
}

// WithNATSConnector 注入 NATS 连接器
func WithNATSConnector(conn connector.NATSConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.NATSConnector = conn
		}
	}
}
