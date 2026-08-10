package idem

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/proto"
)

// Option 组件初始化选项函数
type Option func(*options)

// MiddlewareOption Gin 中间件选项函数
type MiddlewareOption func(*middlewareOptions)

// InterceptorOption gRPC 拦截器选项函数
type InterceptorOption func(*interceptorOptions)

// HTTPIdentityScopeFunc 从已认证的 Gin 上下文中提取稳定的租户或主体作用域。
// 返回空字符串或错误时，中间件会拒绝请求，不会降级到未隔离的幂等 key。
// 认证中间件必须先于 idem 中间件运行。
type HTTPIdentityScopeFunc func(c *gin.Context) (string, error)

// GRPCIdentityScopeFunc 从已认证的 gRPC 上下文中提取稳定的租户或主体作用域。
// 返回空字符串或错误时，拦截器会拒绝请求，不会降级到未隔离的幂等 key。
type GRPCIdentityScopeFunc func(ctx context.Context) (string, error)

// options 组件初始化选项配置（内部使用，小写）
type options struct {
	logger           clog.Logger
	redisConn        connector.RedisConnector
	store            Store
	maxKeyBytes      int
	maxResultBytes   int
	memoryMaxEntries int
}

// WithStore injects a custom persistence implementation. It takes precedence
// over the configured built-in driver and makes the exported Store interface
// usable by external packages.
func WithStore(store Store) Option {
	return func(o *options) {
		if store != nil {
			o.store = store
		}
	}
}

// middlewareOptions Gin 中间件选项配置（内部使用，小写）
type middlewareOptions struct {
	headerKey         string // 幂等键的 HTTP 头名称，默认 "X-Idempotency-Key"
	shouldCache       func(status int) bool
	identityScopeFunc HTTPIdentityScopeFunc
	maxRequestBytes   int64
}

// interceptorOptions gRPC 拦截器选项配置（内部使用，小写）
type interceptorOptions struct {
	metadataKey       string // 幂等键的 gRPC metadata 键名，默认 "x-idem-key"
	shouldCache       func(msg proto.Message) bool
	identityScopeFunc GRPCIdentityScopeFunc
}

// WithLogger 设置 Logger。
func WithLogger(logger clog.Logger) Option {
	return func(o *options) {
		o.logger = logger
	}
}

// WithRedisConnector 注入 Redis 连接器。
func WithRedisConnector(conn connector.RedisConnector) Option {
	return func(o *options) {
		if conn != nil {
			o.redisConn = conn
		}
	}
}

// WithMaxKeyBytes 设置客户端提供的原始幂等 key 的最大字节数。
// 默认上限为 256 字节。非正值会被忽略并保留安全默认值。
func WithMaxKeyBytes(maxBytes int) Option {
	return func(o *options) {
		if maxBytes > 0 {
			o.maxKeyBytes = maxBytes
		}
	}
}

// WithMaxResultBytes 设置单个缓存结果的最大序列化字节数。
// 默认上限为 1 MiB。超限的成功业务结果仍会返回，但不会缓存；非正值会被忽略。
func WithMaxResultBytes(maxBytes int) Option {
	return func(o *options) {
		if maxBytes > 0 {
			o.maxResultBytes = maxBytes
		}
	}
}

// WithMemoryMaxEntries 设置内置 Memory 后端允许保留的最大逻辑 key 数。
// 默认上限为 10000。该选项不影响 Redis 或 WithStore 注入的自定义后端；
// 非正值会被忽略并保留安全默认值。
func WithMemoryMaxEntries(maxEntries int) Option {
	return func(o *options) {
		if maxEntries > 0 {
			o.memoryMaxEntries = maxEntries
		}
	}
}

// WithHeaderKey 设置 Gin 中间件的幂等键 HTTP 头名称。
// 默认为 "X-Idempotency-Key"。
func WithHeaderKey(headerKey string) MiddlewareOption {
	return func(o *middlewareOptions) {
		if headerKey != "" {
			o.headerKey = headerKey
		}
	}
}

// WithHTTPStatusCacheFunc 设置 Gin 中间件的 HTTP 响应缓存策略。
// 返回 true 表示该状态码的响应会被缓存。
func WithHTTPStatusCacheFunc(fn func(status int) bool) MiddlewareOption {
	return func(o *middlewareOptions) {
		if fn != nil {
			o.shouldCache = fn
		}
	}
}

// WithHTTPIdentityScopeFunc 把可信的租户或主体身份同时绑定到 HTTP 幂等
// storage key 和请求 fingerprint。这样即使不同身份复用了相同的客户端 key
// 和请求体，也不会共享锁或缓存结果。
//
// fn 应从认证中间件写入的 Gin context 或 request context 中读取身份，不应直接
// 信任客户端可伪造的普通 header。fn 返回空字符串或错误时请求会 fail closed。
func WithHTTPIdentityScopeFunc(fn HTTPIdentityScopeFunc) MiddlewareOption {
	return func(o *middlewareOptions) {
		if fn != nil {
			o.identityScopeFunc = fn
		}
	}
}

// WithHTTPMaxRequestBytes 设置带幂等 key 的 HTTP 请求体最大字节数。
// 默认上限为 1 MiB。超限请求会在 handler 执行前返回 HTTP 413；
// 非正值会被忽略并保留安全默认值。没有幂等 key 的请求不受此选项影响。
func WithHTTPMaxRequestBytes(maxBytes int64) MiddlewareOption {
	return func(o *middlewareOptions) {
		if maxBytes > 0 {
			o.maxRequestBytes = maxBytes
		}
	}
}

// WithMetadataKey 设置 gRPC 拦截器的幂等键 metadata 键名。
// 默认为 "x-idem-key"。
func WithMetadataKey(metadataKey string) InterceptorOption {
	return func(o *interceptorOptions) {
		if metadataKey != "" {
			o.metadataKey = metadataKey
		}
	}
}

// WithGRPCResponseCacheFunc 设置 gRPC 拦截器的响应缓存策略。
// 只有满足该条件的 proto.Message 成功响应才会被缓存。
func WithGRPCResponseCacheFunc(fn func(msg proto.Message) bool) InterceptorOption {
	return func(o *interceptorOptions) {
		if fn != nil {
			o.shouldCache = fn
		}
	}
}

// WithGRPCIdentityScopeFunc 把可信的租户或主体身份同时绑定到 gRPC 幂等
// storage key 和请求 fingerprint。这样即使不同身份复用了相同的客户端 key
// 和请求消息，也不会共享锁或缓存结果。
//
// fn 应从认证拦截器写入的 context 中读取身份，不应直接信任客户端可伪造的
// metadata。fn 返回空字符串或错误时请求会 fail closed。
func WithGRPCIdentityScopeFunc(fn GRPCIdentityScopeFunc) InterceptorOption {
	return func(o *interceptorOptions) {
		if fn != nil {
			o.identityScopeFunc = fn
		}
	}
}
