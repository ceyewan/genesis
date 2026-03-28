package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器。
// 它会为每个 gRPC 调用提供熔断保护，并区分系统性错误与明显业务错误，
// 以避免把 InvalidArgument、NotFound 等业务错误直接计入熔断统计。
//
// 使用示例:
//
//	// 默认行为（服务级别熔断），key 为 cc.Target()，即服务地址
//	conn, _ := grpc.NewClient(
//	    "localhost:9001",
//	    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
//	)
func (cb *circuitBreaker) UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor {
	// 默认使用服务级别 Key
	cfg := &interceptorConfig{keyFunc: defaultKeyFunc}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 使用配置的 KeyFunc 生成熔断 Key
		key := cfg.keyFunc(ctx, method, cc)

		if cb.logger != nil {
			cb.logger.Debug("unary call with circuit breaker",
				clog.String("key", key),
				clog.String("method", method))
		}

		// 使用熔断器执行调用
		var callErr error
		_, err := cb.Execute(ctx, key, func() (any, error) {
			callErr = invoker(ctx, method, req, reply, cc, opts...)
			if shouldCountGRPCFailure(callErr) {
				return nil, callErr
			}
			return nil, nil
		})
		if err == nil {
			return callErr
		}

		return err
	}
}

func defaultKeyFunc(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
	return cc.Target()
}

func shouldCountGRPCFailure(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case err == context.Canceled:
		return false
	case err == context.DeadlineExceeded:
		return false
	}

	st, ok := status.FromError(err)
	if !ok {
		return true
	}

	switch st.Code() {
	case codes.OK,
		codes.Canceled,
		codes.InvalidArgument,
		codes.NotFound,
		codes.AlreadyExists,
		codes.PermissionDenied,
		codes.FailedPrecondition,
		codes.Aborted,
		codes.OutOfRange,
		codes.Unauthenticated:
		return false
	case codes.Unknown,
		codes.DeadlineExceeded,
		codes.ResourceExhausted,
		codes.Unimplemented,
		codes.Internal,
		codes.Unavailable,
		codes.DataLoss:
		return true
	default:
		return true
	}
}
