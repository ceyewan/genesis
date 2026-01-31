package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"

	"google.golang.org/grpc"
)

// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
// 为每个 gRPC 调用提供熔断保护，支持 InterceptorOption 配置 Key 生成策略
//
// 使用示例:
//
//	// 默认行为（服务级别熔断）
//	conn, _ := grpc.NewClient(
//	    "localhost:9001",
//	    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
//	)
//
//	// 自定义 Key（例如使用方法名）
//	conn, _ := grpc.NewClient(
//	    "etcd:///logic-service",
//	    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
//	        breaker.WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
//	            return fullMethod
//	        }),
//	    )),
//	)
func (cb *circuitBreaker) UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor {
	// 默认使用服务级别 Key
	cfg := &interceptorConfig{keyFunc: defaultKeyFunc}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 使用配置的 KeyFunc 生成熔断 Key
		key := cfg.keyFunc(ctx, method, cc)

		if cb.logger != nil {
			cb.logger.Debug("unary call with circuit breaker",
				clog.String("key", key),
				clog.String("method", method))
		}

		// 使用熔断器执行调用
		_, err := cb.Execute(ctx, key, func() (interface{}, error) {
			err := invoker(ctx, method, req, reply, cc, opts...)
			return nil, err
		})

		return err
	}
}

func defaultKeyFunc(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
	return cc.Target()
}
