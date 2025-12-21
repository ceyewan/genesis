package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"google.golang.org/grpc"
)

// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
// 为每个 gRPC 调用提供熔断保护
//
// 使用示例:
//
//	brk, _ := breaker.New(cfg, breaker.WithLogger(logger))
//	conn, _ := grpc.Dial(
//		"localhost:9001",
//		grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
//	)
func (cb *circuitBreaker) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// 从连接中提取服务名（使用 target 作为服务名）
		serviceName := cc.Target()

		if cb.logger != nil {
			cb.logger.Debug("unary call with circuit breaker",
				clog.String("service", serviceName),
				clog.String("method", method))
		}

		// 使用熔断器执行调用
		_, err := cb.Execute(ctx, serviceName, func() (interface{}, error) {
			err := invoker(ctx, method, req, reply, cc, opts...)
			return nil, err
		})

		// 记录方法级别的指标
		if cb.meter != nil {
			result := "success"
			if err != nil {
				result = "failure"
			}

			if counter, e := cb.meter.Counter(MetricRequestsTotal, "Total requests"); e == nil && counter != nil {
				counter.Add(ctx, 1,
					metrics.L(LabelService, serviceName),
					metrics.L(LabelMethod, method),
					metrics.L(LabelResult, result))
			}
		}

		return err
	}
}

// StreamClientInterceptor 返回 gRPC 流式调用客户端拦截器
// 为流式 gRPC 调用提供熔断保护
//
// 使用示例:
//
//	brk, _ := breaker.New(cfg, breaker.WithLogger(logger))
//	conn, _ := grpc.Dial(
//		"localhost:9001",
//		grpc.WithStreamInterceptor(brk.StreamClientInterceptor()),
//	)
func (cb *circuitBreaker) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		// 从连接中提取服务名
		serviceName := cc.Target()

		if cb.logger != nil {
			cb.logger.Debug("stream call with circuit breaker",
				clog.String("service", serviceName),
				clog.String("method", method))
		}

		// 使用熔断器执行流式调用
		result, err := cb.Execute(ctx, serviceName, func() (interface{}, error) {
			stream, err := streamer(ctx, desc, cc, method, opts...)
			return stream, err
		})

		if err != nil {
			// 记录失败指标
			if cb.meter != nil {
				if counter, e := cb.meter.Counter(MetricRequestsTotal, "Total requests"); e == nil && counter != nil {
					counter.Add(ctx, 1,
						metrics.L(LabelService, serviceName),
						metrics.L(LabelMethod, method),
						metrics.L(LabelResult, "failure"))
				}
			}
			return nil, err
		}

		// 记录成功指标
		if cb.meter != nil {
			if counter, e := cb.meter.Counter(MetricRequestsTotal, "Total requests"); e == nil && counter != nil {
				counter.Add(ctx, 1,
					metrics.L(LabelService, serviceName),
					metrics.L(LabelMethod, method),
					metrics.L(LabelResult, "success"))
			}
		}

		return result.(grpc.ClientStream), nil
	}
}

