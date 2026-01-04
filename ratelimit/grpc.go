package ratelimit

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
	keyFunc func(ctx context.Context, fullMethod string) string,
	limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.UnaryServerInterceptor {
	if limiter == nil {
		limiter = Discard()
	}
	if keyFunc == nil {
		keyFunc = defaultGRPCKeyFunc
	}
	if limitFunc == nil {
		limitFunc = func(ctx context.Context, fullMethod string) Limit {
			return Limit{}
		}
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
			return nil, status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
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
//   - keyFunc: 从请求中提取限流键的函数，如果为 nil，默认使用 fullMethod
//   - limitFunc: 获取限流规则的函数
//
// 使用示例:
//
//	conn, _ := grpc.Dial(
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
	keyFunc func(ctx context.Context, fullMethod string) string,
	limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.UnaryClientInterceptor {
	if limiter == nil {
		limiter = Discard()
	}
	if keyFunc == nil {
		keyFunc = defaultGRPCKeyFunc
	}
	if limitFunc == nil {
		limitFunc = func(ctx context.Context, fullMethod string) Limit {
			return Limit{}
		}
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
			return status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
		}

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// ========================================
// 流式拦截器 (Stream Interceptor)
// ========================================

// StreamServerInterceptor 返回 gRPC 流式调用服务端拦截器
// 默认在每次 RecvMsg 前进行限流检查；keyFunc 为空时使用 fullMethod
func StreamServerInterceptor(
	limiter Limiter,
	keyFunc func(ctx context.Context, fullMethod string) string,
	limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.StreamServerInterceptor {
	if limiter == nil {
		limiter = Discard()
	}
	if keyFunc == nil {
		keyFunc = defaultGRPCKeyFunc
	}
	if limitFunc == nil {
		limitFunc = func(ctx context.Context, fullMethod string) Limit {
			return Limit{}
		}
	}

	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		wrapped := &serverStreamWrapper{
			ServerStream: stream,
			limiter:      limiter,
			keyFunc:      keyFunc,
			limitFunc:    limitFunc,
			fullMethod:   info.FullMethod,
		}
		return handler(srv, wrapped)
	}
}

// StreamClientInterceptor 返回 gRPC 流式调用客户端拦截器
// 默认在每次 SendMsg 前进行限流检查；keyFunc 为空时使用 fullMethod
func StreamClientInterceptor(
	limiter Limiter,
	keyFunc func(ctx context.Context, fullMethod string) string,
	limitFunc func(ctx context.Context, fullMethod string) Limit,
) grpc.StreamClientInterceptor {
	if limiter == nil {
		limiter = Discard()
	}
	if keyFunc == nil {
		keyFunc = defaultGRPCKeyFunc
	}
	if limitFunc == nil {
		limitFunc = func(ctx context.Context, fullMethod string) Limit {
			return Limit{}
		}
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			return nil, err
		}
		return &clientStreamWrapper{
			ClientStream: clientStream,
			limiter:      limiter,
			keyFunc:      keyFunc,
			limitFunc:    limitFunc,
			fullMethod:   method,
		}, nil
	}
}

func defaultGRPCKeyFunc(ctx context.Context, fullMethod string) string {
	return fullMethod
}

type serverStreamWrapper struct {
	grpc.ServerStream
	limiter    Limiter
	keyFunc    func(ctx context.Context, fullMethod string) string
	limitFunc  func(ctx context.Context, fullMethod string) Limit
	fullMethod string
}

func (s *serverStreamWrapper) RecvMsg(m interface{}) error {
	key := s.keyFunc(s.Context(), s.fullMethod)
	limit := s.limitFunc(s.Context(), s.fullMethod)
	if limit.Rate <= 0 || limit.Burst <= 0 {
		return s.ServerStream.RecvMsg(m)
	}

	allowed, err := s.limiter.Allow(s.Context(), key, limit)
	if err != nil {
		return s.ServerStream.RecvMsg(m)
	}
	if !allowed {
		return status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
	}
	return s.ServerStream.RecvMsg(m)
}

type clientStreamWrapper struct {
	grpc.ClientStream
	limiter    Limiter
	keyFunc    func(ctx context.Context, fullMethod string) string
	limitFunc  func(ctx context.Context, fullMethod string) Limit
	fullMethod string
}

func (s *clientStreamWrapper) SendMsg(m interface{}) error {
	key := s.keyFunc(s.Context(), s.fullMethod)
	limit := s.limitFunc(s.Context(), s.fullMethod)
	if limit.Rate <= 0 || limit.Burst <= 0 {
		return s.ClientStream.SendMsg(m)
	}

	allowed, err := s.limiter.Allow(s.Context(), key, limit)
	if err != nil {
		return s.ClientStream.SendMsg(m)
	}
	if !allowed {
		return status.Error(codes.ResourceExhausted, ErrRateLimitExceeded.Error())
	}
	return s.ClientStream.SendMsg(m)
}
