package idgen

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// Option 组件初始化选项函数
type Option func(*options)

// options 组件初始化选项配置（内部使用）
type options struct {
	Logger         clog.Logger
	RedisConnector connector.RedisConnector
	EtcdConnector  connector.EtcdConnector
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.Logger = logger.WithNamespace("idgen")
		} else {
			o.Logger = clog.Discard()
		}
	}
}

// WithRedisConnector 注入 Redis 连接器
// 用于 Allocator、Sequencer 等组件
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.RedisConnector = conn
		}
	}
}

// WithEtcdConnector 注入 Etcd 连接器
// 目前仅用于 Allocator 组件
func WithEtcdConnector(conn connector.EtcdConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.EtcdConnector = conn
		}
	}
}
