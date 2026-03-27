package ratelimit

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ceyewan/genesis/clog"
)

// ========================================
// 类型定义 (Type Definitions)
// ========================================

// GRPCKeyFunc 从请求中提取限流键的函数类型
type GRPCKeyFunc func(ctx context.Context, fullMethod string) string

// GRPCLimitFunc 获取限流规则的函数类型
type GRPCLimitFunc func(ctx context.Context, fullMethod string) Limit

// GRPCInterceptorOptions 定义 gRPC 限流拦截器的可选行为。
type GRPCInterceptorOptions struct {
	ErrorPolicy ErrorPolicy
	Logger      clog.Logger
}

// grpcLimiterConfig gRPC 限流器内部配置（复用逻辑）
type grpcLimiterConfig struct {
	limiter     Limiter
	keyFunc     GRPCKeyFunc
	limitFunc   GRPCLimitFunc
	errorPolicy ErrorPolicy
	logger      clog.Logger
}

// newGRPCLimiterConfig 创建标准化的 gRPC 限流配置
func newGRPCLimiterConfig(limiter Limiter, keyFunc GRPCKeyFunc, limitFunc GRPCLimitFunc, opts *GRPCInterceptorOptions) *grpcLimiterConfig {
	cfg := &grpcLimiterConfig{
		limiter:     limiter,
		keyFunc:     keyFunc,
		limitFunc:   limitFunc,
		errorPolicy: ErrorPolicyFailOpen,
	}
	if cfg.limiter == nil {
		cfg.limiter = Discard()
	}
	if cfg.keyFunc == nil {
		cfg.keyFunc = defaultGRPCKeyFunc
	}
	if cfg.limitFunc == nil {
		cfg.limitFunc = func(ctx context.Context, fullMethod string) Limit {
			return Limit{}
		}
	}
	if opts != nil {
		if opts.ErrorPolicy != "" {
			cfg.errorPolicy = opts.ErrorPolicy
		}
		cfg.logger = opts.Logger
	}
	return cfg
}

// check 执行限流检查。
// 返回值：
//   - allowed=true: 请求被允许
//   - allowed=false, passThrough=true: 限流器出错且策略为 fail-open，或规则无效
//   - allowed=false, passThrough=false, err=nil: 请求被限流
//   - allowed=false, passThrough=false, err!=nil: 限流器出错且策略为 fail-closed
func (c *grpcLimiterConfig) check(ctx context.Context, fullMethod string) (allowed bool, passThrough bool, err error) {
	key := c.keyFunc(ctx, fullMethod)
	limit := c.limitFunc(ctx, fullMethod)

	// 无效限流规则，放行
	if limit.Rate <= 0 || limit.Burst <= 0 {
		return false, true, nil
	}

	// 执行限流检查
	allowed, err = c.limiter.Allow(ctx, key, limit)
	if err != nil {
		if c.logger != nil {
			c.logger.Warn("gRPC rate limiter check failed",
				clog.String("full_method", fullMethod),
				clog.String("key", key),
				clog.String("error_policy", string(c.errorPolicy)),
				clog.Error(err))
		}
		if c.errorPolicy == ErrorPolicyFailClosed {
			return false, false, err
		}
		return false, true, nil
	}

	return allowed, false, nil
}

// ========================================
// 服务端拦截器 (Server Interceptor)
// ========================================

// UnaryServerInterceptor 返回 gRPC 一元调用服务端拦截器
//
// 参数:
//   - limiter: 限流器实例
//   - keyFunc: 从请求中提取限流键的函数，如果为 nil，默认使用 fullMethod
//   - limitFunc: 获取限流规则的函数
//
// 使用示例:
//
//	server := grpc.NewServer(
//	    grpc.ChainUnaryInterceptor(
//	        ratelimit.UnaryServerInterceptor(limiter,
//	            nil,
//	            func(ctx context.Context, fullMethod string) ratelimit.Limit {
//	                return ratelimit.Limit{Rate: 100, Burst: 200}
//	            }),
//	    ),
//	)
func UnaryServerInterceptor(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
) grpc.UnaryServerInterceptor {
	return UnaryServerInterceptorWithOptions(limiter, keyFunc, limitFunc, nil)
}

// UnaryServerInterceptorWithOptions 返回带错误策略的 gRPC 一元调用服务端拦截器。
func UnaryServerInterceptorWithOptions(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
	opts *GRPCInterceptorOptions,
) grpc.UnaryServerInterceptor {
	cfg := newGRPCLimiterConfig(limiter, keyFunc, limitFunc, opts)

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		allowed, passThrough, err := cfg.check(ctx, info.FullMethod)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "rate limiter unavailable")
		}
		if passThrough || allowed {
			return handler(ctx, req)
		}
		return nil, status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
	}
}

// ========================================
// 客户端拦截器 (Client Interceptor)
// ========================================

// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
//
// 参数:
//   - limiter: 限流器实例
//   - keyFunc: 从请求中提取限流键的函数，如果为 nil，默认使用 fullMethod
//   - limitFunc: 获取限流规则的函数
//
// 使用示例:
//
//	conn, _ := grpc.NewClient(
//	    "localhost:9001",
//	    grpc.WithUnaryInterceptor(
//	        ratelimit.UnaryClientInterceptor(limiter,
//	            nil,
//	            func(ctx context.Context, fullMethod string) ratelimit.Limit {
//	                return ratelimit.Limit{Rate: 100, Burst: 200}
//	            }),
//	    ),
//	)
func UnaryClientInterceptor(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
) grpc.UnaryClientInterceptor {
	return UnaryClientInterceptorWithOptions(limiter, keyFunc, limitFunc, nil)
}

// UnaryClientInterceptorWithOptions 返回带错误策略的 gRPC 一元调用客户端拦截器。
func UnaryClientInterceptorWithOptions(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
	opts *GRPCInterceptorOptions,
) grpc.UnaryClientInterceptor {
	cfg := newGRPCLimiterConfig(limiter, keyFunc, limitFunc, opts)

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		allowed, passThrough, err := cfg.check(ctx, method)
		if err != nil {
			return status.Error(codes.Unavailable, "rate limiter unavailable")
		}
		if passThrough || allowed {
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		return status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
	}
}

// ========================================
// 流式拦截器 (Stream Interceptor)
// ========================================

// StreamServerInterceptor 返回 gRPC 流式调用服务端拦截器
// 在流建立时进行一次限流检查（Per-Stream 限流）；keyFunc 为空时使用 fullMethod
//
// 注意：采用 Per-Stream 限流而非 Per-Message 限流，原因：
// 1. 流式请求通常是高频场景，Per-Message 会快速耗尽令牌
// 2. 避免流中途被限流中断，导致不可预期的错误
func StreamServerInterceptor(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
) grpc.StreamServerInterceptor {
	return StreamServerInterceptorWithOptions(limiter, keyFunc, limitFunc, nil)
}

// StreamServerInterceptorWithOptions 返回带错误策略的 gRPC 流式调用服务端拦截器。
func StreamServerInterceptorWithOptions(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
	opts *GRPCInterceptorOptions,
) grpc.StreamServerInterceptor {
	cfg := newGRPCLimiterConfig(limiter, keyFunc, limitFunc, opts)

	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		allowed, passThrough, err := cfg.check(stream.Context(), info.FullMethod)
		if err != nil {
			return status.Error(codes.Unavailable, "rate limiter unavailable")
		}
		if passThrough || allowed {
			return handler(srv, stream)
		}
		return status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
	}
}

// StreamClientInterceptor 返回 gRPC 流式调用客户端拦截器
// 在流建立时进行一次限流检查（Per-Stream 限流）；keyFunc 为空时使用 fullMethod
//
// 注意：采用 Per-Stream 限流而非 Per-Message 限流，原因：
// 1. 流式请求通常是高频场景，Per-Message 会快速耗尽令牌
// 2. 避免流中途被限流中断，导致不可预期的错误
func StreamClientInterceptor(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
) grpc.StreamClientInterceptor {
	return StreamClientInterceptorWithOptions(limiter, keyFunc, limitFunc, nil)
}

// StreamClientInterceptorWithOptions 返回带错误策略的 gRPC 流式调用客户端拦截器。
func StreamClientInterceptorWithOptions(
	limiter Limiter,
	keyFunc GRPCKeyFunc,
	limitFunc GRPCLimitFunc,
	opts *GRPCInterceptorOptions,
) grpc.StreamClientInterceptor {
	cfg := newGRPCLimiterConfig(limiter, keyFunc, limitFunc, opts)

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		allowed, passThrough, err := cfg.check(ctx, method)
		if err != nil {
			return nil, status.Error(codes.Unavailable, "rate limiter unavailable")
		}
		if passThrough || allowed {
			return streamer(ctx, desc, cc, method, opts...)
		}
		return nil, status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
	}
}

func defaultGRPCKeyFunc(ctx context.Context, fullMethod string) string {
	return fullMethod
}
