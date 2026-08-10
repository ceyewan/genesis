package breaker

import (
	"context"
	"errors"
	"reflect"

	"github.com/ceyewan/genesis/clog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
		result, err := cb.Execute(ctx, key, func() (any, error) {
			callErr = invoker(ctx, method, req, reply, cc, opts...)
			if shouldCountGRPCFailure(callErr) {
				return nil, callErr
			}
			return nil, nil
		})
		if err != nil {
			return err
		}

		if callErr != nil {
			return callErr
		}

		return applyFallbackResult(reply, result)
	}
}

// applyFallbackResult 将 Execute 的 fallback 成功结果写入 gRPC reply。
// invoker 正常执行时 result 为 nil，因此不会触碰已经填充的 reply。
func applyFallbackResult(reply, result any) error {
	if result == nil {
		return nil
	}
	if reply == nil {
		return status.Errorf(codes.Internal, "breaker: fallback result type %T cannot be applied to a nil reply", result)
	}

	dstProto, dstIsProto := reply.(proto.Message)
	srcProto, srcIsProto := result.(proto.Message)
	if dstIsProto || srcIsProto {
		if !dstIsProto || !srcIsProto || reflect.TypeOf(dstProto) != reflect.TypeOf(srcProto) {
			return fallbackResultTypeError(reply, result)
		}

		dstValue := reflect.ValueOf(dstProto)
		srcValue := reflect.ValueOf(srcProto)
		if (dstValue.Kind() == reflect.Pointer && dstValue.IsNil()) ||
			(srcValue.Kind() == reflect.Pointer && srcValue.IsNil()) {
			return fallbackResultTypeError(reply, result)
		}

		cloned := proto.Clone(srcProto)
		proto.Reset(dstProto)
		proto.Merge(dstProto, cloned)
		return nil
	}

	dst := reflect.ValueOf(reply)
	if dst.Kind() != reflect.Pointer || dst.IsNil() {
		return fallbackResultTypeError(reply, result)
	}

	src := reflect.ValueOf(result)
	if src.IsValid() && src.Type().AssignableTo(dst.Elem().Type()) {
		dst.Elem().Set(src)
		return nil
	}
	if src.Kind() == reflect.Pointer && !src.IsNil() && src.Elem().Type().AssignableTo(dst.Elem().Type()) {
		dst.Elem().Set(src.Elem())
		return nil
	}

	return fallbackResultTypeError(reply, result)
}

func fallbackResultTypeError(reply, result any) error {
	return status.Errorf(
		codes.Internal,
		"breaker: fallback result type %T is incompatible with reply type %T",
		result,
		reply,
	)
}

func defaultKeyFunc(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
	return cc.Target()
}

func shouldCountGRPCFailure(err error) bool {
	if err == nil {
		return false
	}

	switch {
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, context.DeadlineExceeded):
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
