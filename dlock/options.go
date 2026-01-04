package dlock

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// Option DLock 组件初始化选项函数
type Option func(*options)

// options 选项结构（内部使用，小写）
type options struct {
	logger         clog.Logger
	redisConnector connector.RedisConnector
	etcdConnector  connector.EtcdConnector
}

// WithLogger 注入日志记录器
// 组件会自动添加 component=dlock 字段
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithRedisConnector 注入 Redis 连接器
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.redisConnector = conn
		}
	}
}

// WithEtcdConnector 注入 Etcd 连接器
func WithEtcdConnector(conn connector.EtcdConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.etcdConnector = conn
		}
	}
}
