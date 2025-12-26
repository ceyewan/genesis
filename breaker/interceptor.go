package breaker

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"

	"google.golang.org/grpc"
)

// UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
// 为每个 gRPC 调用提供熔断保护，支持 InterceptorOption 配置 Key 生成策略
//
// 使用示例:
//
//	// 默认行为（服务级别熔断）
//	conn, _ := grpc.Dial(
//	    "localhost:9001",
//	    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
//	)
//
//	// 后端级别熔断（推荐用于负载均衡场景）
//	conn, _ := grpc.Dial(
//	    "etcd:///logic-service",
//	    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
//	        breaker.WithBackendLevelKey(),
//	    )),
//	)
func (cb *circuitBreaker) UnaryClientInterceptor(opts ...InterceptorOption) grpc.UnaryClientInterceptor {
	// 默认使用服务级别 Key
	cfg := &interceptorConfig{keyFunc: ServiceLevelKey()}
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

		// 记录方法级别的指标
		if cb.meter != nil {
			result := "success"
			if err != nil {
				result = "failure"
			}

			if counter, e := cb.meter.Counter(MetricRequestsTotal, "Total requests"); e == nil && counter != nil {
				counter.Add(ctx, 1,
					metrics.L(LabelService, key),
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
