package adapter

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ceyewan/genesis/breaker/types"
)

// InterceptorOption gRPC 拦截器配置选项
type InterceptorOption func(*interceptorConfig)

// interceptorConfig gRPC 拦截器配置
type interceptorConfig struct {
	// FallbackHandler 全局降级处理器（可选）
	// 当熔断时调用，可返回降级数据
	FallbackHandler func(context.Context, string, error) error

	// ShouldCount 自定义判断哪些错误应计入失败
	// 默认: codes.Unavailable, codes.DeadlineExceeded, codes.Internal
	ShouldCount func(error) bool
}

// UnaryClientInterceptor 创建 gRPC 客户端熔断拦截器
// b: 熔断器实例
// opts: 可选配置
func UnaryClientInterceptor(b types.Breaker, opts ...InterceptorOption) grpc.UnaryClientInterceptor {
	cfg := defaultInterceptorConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		// 1. 从 method 提取服务名
		// 例如: "/user.v1.UserService/GetUser" -> "user.v1.UserService"
		serviceName := extractServiceName(method)

		// 2. 使用熔断器保护
		var err error
		if cfg.FallbackHandler != nil {
			// 使用带降级的执行
			err = b.ExecuteWithFallback(ctx, serviceName, func() error {
				return invoker(ctx, method, req, reply, cc, opts...)
			}, func(cbErr error) error {
				return cfg.FallbackHandler(ctx, method, cbErr)
			})
		} else {
			// 普通执行
			err = b.Execute(ctx, serviceName, func() error {
				return invoker(ctx, method, req, reply, cc, opts...)
			})
		}

		// 3. 如果是熔断错误，转换为 gRPC 错误码
		if err == types.ErrOpenState {
			return status.Error(codes.Unavailable, "circuit breaker open")
		}

		return err
	}
}

// extractServiceName 从 gRPC Method 提取服务名
// "/user.v1.UserService/GetUser" -> "user.v1.UserService"
func extractServiceName(method string) string {
	// method 格式: /<package>.<service>/<method>
	if len(method) == 0 || method[0] != '/' {
		return method
	}

	// 去掉开头的 '/'
	method = method[1:]

	// 找到最后一个 '/' 的位置
	if idx := strings.LastIndex(method, "/"); idx != -1 {
		return method[:idx]
	}

	return method
}

// WithFallbackHandler 设置全局降级处理器
func WithFallbackHandler(h func(context.Context, string, error) error) InterceptorOption {
	return func(c *interceptorConfig) {
		c.FallbackHandler = h
	}
}

// WithShouldCount 自定义失败判断逻辑
func WithShouldCount(fn func(error) bool) InterceptorOption {
	return func(c *interceptorConfig) {
		c.ShouldCount = fn
	}
}

// defaultInterceptorConfig 返回默认拦截器配置
func defaultInterceptorConfig() *interceptorConfig {
	return &interceptorConfig{
		ShouldCount: func(err error) bool {
			if err == nil {
				return false
			}
			st, ok := status.FromError(err)
			if !ok {
				return true
			}
			// 只统计这些错误码
			code := st.Code()
			return code == codes.Unavailable ||
				code == codes.DeadlineExceeded ||
				code == codes.Internal ||
				code == codes.Unknown
		},
	}
}
