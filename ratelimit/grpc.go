package ratelimit

import (
	"context"
	"strings"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

// KeyFunc 限流键生成函数类型
// 从 gRPC 调用上下文中提取限流 Key
type KeyFunc func(ctx context.Context, fullMethod string) string

// ========================================
// 服务端拦截器 (Server Interceptor)
// ========================================

// UnaryServerInterceptor 返回 gRPC 一元调用服务端拦截器
//
// 参数:
//   - limiter: 限流器实例
//   - keyFunc: 从请求中提取限流键的函数，如果为 nil，默认使用方法级别 Key
//   - limitFunc: 获取限流规则的函数
//
// 使用示例:
//
//	server := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        ratelimit.UnaryServerInterceptor(limiter,
//	            ratelimit.MethodLevelKey(),
//	            func(ctx context.Context, fullMethod string) ratelimit.Limit {
//	                return ratelimit.Limit{Rate: 100, Burst: 200}
//	            }),
//	    ),
//	)
func UnaryServerInterceptor(
	limiter Limiter,
	keyFunc KeyFunc,
	limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.UnaryServerInterceptor {
	if keyFunc == nil {
		keyFunc = MethodLevelKey()
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 提取限流键
		key := keyFunc(ctx, info.FullMethod)

		// 获取限流规则
		limit := limitFunc(ctx, info.FullMethod)

		// 如果限流规则无效，直接放行
		if limit.Rate <= 0 || limit.Burst <= 0 {
			return handler(ctx, req)
		}

		// 检查是否允许请求
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			// 限流器出错时放行，避免影响业务
			return handler(ctx, req)
		}

		if !allowed {
			// 被限流，返回错误
			return nil, ErrRateLimitExceeded
		}

		return handler(ctx, req)
	}
}

// ========================================
// 客户端拦截器 (Client Interceptor)
// ========================================

// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
//
// 参数:
//   - limiter: 限流器实例
//   - keyFunc: 从请求中提取限流键的函数，如果为 nil，默认使用方法级别 Key
//   - limitFunc: 获取限流规则的函数
//
// 使用示例:
//
//	conn, _ := grpc.Dial(
//	    "localhost:9001",
//	    grpc.WithUnaryInterceptor(
//	        ratelimit.UnaryClientInterceptor(limiter,
//	            ratelimit.MethodLevelKey(),
//	            func(ctx context.Context, fullMethod string) ratelimit.Limit {
//	                return ratelimit.Limit{Rate: 100, Burst: 200}
//	            }),
//	    ),
//	)
func UnaryClientInterceptor(
	limiter Limiter,
	keyFunc KeyFunc,
	limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.UnaryClientInterceptor {
	if keyFunc == nil {
		keyFunc = MethodLevelKey()
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 提取限流键
		key := keyFunc(ctx, method)

		// 获取限流规则
		limit := limitFunc(ctx, method)

		// 如果限流规则无效，直接放行
		if limit.Rate <= 0 || limit.Burst <= 0 {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		// 检查是否允许请求
		allowed, err := limiter.Allow(ctx, key, limit)
		if err != nil {
			// 限流器出错时放行，避免影响业务
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		if !allowed {
			// 被限流，返回错误
			return ErrRateLimitExceeded
		}

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// ========================================
// 内置 KeyFunc 实现
// ========================================

// ServiceLevelKey 服务级别限流键
// 从 fullMethod 中提取服务名部分
// gRPC 方法名格式: "/pkg.Service/Method"
// 返回: "pkg.Service"
func ServiceLevelKey() KeyFunc {
	return func(ctx context.Context, fullMethod string) string {
		// fullMethod 格式: "/pkg.Service/Method"
		// 去掉开头的 "/" 后按 "/" 分割，取第二部分（服务名）
		parts := strings.Split(fullMethod, "/")
		if len(parts) >= 2 {
			return parts[1] // 返回 "pkg.Service"
		}
		return fullMethod
	}
}

// MethodLevelKey 方法级别限流键
// 返回完整的方法名: "/pkg.Service/Method"
func MethodLevelKey() KeyFunc {
	return func(ctx context.Context, fullMethod string) string {
		return fullMethod
	}
}

// IPLevelKey IP 级别限流键
// 从 gRPC Peer 信息中提取客户端 IP 地址
// 返回: "ip:10.0.0.1"
// 如果无法获取 IP，返回: "ip:unknown"
func IPLevelKey() KeyFunc {
	return func(ctx context.Context, fullMethod string) string {
		// 从 peer.Context 中获取客户端地址
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			return "ip:" + p.Addr.String()
		}
		return "ip:unknown"
	}
}

// CompositeKey 组合多维度
// 返回: "service:pkg.Service:method:/pkg.Service/Method"
func CompositeKey(keyFuncs ...KeyFunc) KeyFunc {
	return func(ctx context.Context, fullMethod string) string {
		var result string
		for i, kf := range keyFuncs {
			if i > 0 {
				result += ":"
			}
			result += kf(ctx, fullMethod)
		}
		return result
	}
}

// ========================================
// 日志记录辅助
// ========================================

// WithKeyLogger 创建带日志记录的拦截器包装器
// 可用于调试和监控限流行为
func WithKeyLogger(logger clog.Logger, keyFunc KeyFunc) KeyFunc {
	if logger == nil {
		return keyFunc
	}
	return func(ctx context.Context, fullMethod string) string {
		key := keyFunc(ctx, fullMethod)
		logger.Debug("rate limit key generated",
			clog.String("method", fullMethod),
			clog.String("key", key))
		return key
	}
}
