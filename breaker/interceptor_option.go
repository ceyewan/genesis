package breaker

import (
	"context"

	"google.golang.org/grpc"
)

// KeyFunc 从 gRPC 调用上下文中提取熔断 Key
type KeyFunc func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string

// InterceptorOption 拦截器选项函数类型
type InterceptorOption func(*interceptorConfig)

// interceptorConfig 拦截器内部配置（非导出）
type interceptorConfig struct {
	keyFunc KeyFunc
}

// WithKeyFunc 设置 Key 生成函数
func WithKeyFunc(fn KeyFunc) InterceptorOption {
	return func(cfg *interceptorConfig) {
		cfg.keyFunc = fn
	}
}
