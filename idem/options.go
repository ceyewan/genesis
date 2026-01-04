package idem

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// Option 组件初始化选项函数
type Option func(*options)

// MiddlewareOption Gin 中间件选项函数
type MiddlewareOption func(*middlewareOptions)

// InterceptorOption gRPC 拦截器选项函数
type InterceptorOption func(*interceptorOptions)

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger    clog.Logger
	redisConn connector.RedisConnector
}

// middlewareOptions Gin 中间件选项配置（内部使用，小写）
type middlewareOptions struct {
	headerKey string // 幂等键的 HTTP 头名称，默认 "X-Idempotency-Key"
}

// interceptorOptions gRPC 拦截器选项配置（内部使用，小写）
type interceptorOptions struct {
	metadataKey string // 幂等键的 gRPC metadata 键名，默认 "x-idem-key"
}

// WithLogger 设置 Logger
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithRedisConnector 注入 Redis 连接器
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.redisConn = conn
		}
	}
}

// WithHeaderKey 设置 Gin 中间件的幂等键 HTTP 头名称
// 默认为 "X-Idempotency-Key"
func WithHeaderKey(headerKey string) MiddlewareOption {
	return func(o *middlewareOptions) {
		if headerKey != "" {
			o.headerKey = headerKey
		}
	}
}

// WithMetadataKey 设置 gRPC 拦截器的幂等键 metadata 键名
// 默认为 "x-idem-key"
func WithMetadataKey(metadataKey string) InterceptorOption {
	return func(o *interceptorOptions) {
		if metadataKey != "" {
			o.metadataKey = metadataKey
		}
	}
}
