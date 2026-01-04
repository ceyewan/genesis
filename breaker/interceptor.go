package breaker

import (
	"context"
	"io"

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

// StreamClientInterceptor 返回 gRPC 流式调用客户端拦截器
// 支持 InterceptorOption 配置 Key 生成策略
//
// 流式熔断策略：
// 1. 在创建流时进行熔断检查 (BreakOnCreate，默认开启)
// 2. 在每次 SendMsg/RecvMsg 时进行熔断检查 (BreakOnMessage，默认关闭)
// 3. io.EOF 不计为失败，其他错误是否计为失败由 FailureClassifier 决定
func (cb *circuitBreaker) StreamClientInterceptor(opts ...InterceptorOption) grpc.StreamClientInterceptor {
	// 默认值：
	// breakOnCreate: true (建流熔断)
	// breakOnMessage: false (消息不熔断)
	// failureClassifier: 除 EOF 外都计失败
	cfg := &interceptorConfig{
		keyFunc:        defaultKeyFunc,
		breakOnCreate:  true,
		breakOnMessage: false,
		failureClassifier: func(err error) bool {
			return err != nil && err != io.EOF
		},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// 兜底保护：防止 options 覆盖导致 nil
	if cfg.failureClassifier == nil {
		cfg.failureClassifier = func(err error) bool {
			return err != nil && err != io.EOF
		}
	}

	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		key := cfg.keyFunc(ctx, method, cc)

		if cb.logger != nil {
			cb.logger.Debug("stream call with circuit breaker",
				clog.String("key", key),
				clog.String("method", method))
		}

		var stream grpc.ClientStream
		var err error

		// 1. 在创建流时进行熔断检查 (BreakOnCreate)
		if cfg.breakOnCreate {
			// 使用 Execute 以支持 Fallback (虽然建流一般不需要 fallback，但保持一致性)
			_, err = cb.Execute(ctx, key, func() (interface{}, error) {
				var innerErr error
				stream, innerErr = streamer(ctx, desc, cc, method, opts...)
				// 这里的错误会触发熔断计数
				return nil, innerErr
			})
		} else {
			// 不熔断，直接创建流
			stream, err = streamer(ctx, desc, cc, method, opts...)
		}

		if err != nil {
			return nil, err
		}

		// 兜底检查：如果 fallback 吞掉了错误导致 err=nil 但 stream 仍为 nil
		// 必须返回错误，否则会导致后续空指针 panic
		if stream == nil {
			if cb.logger != nil {
				cb.logger.Warn("stream is nil after circuit breaker execution (fallback might have swallowed error)",
					clog.String("key", key))
			}
			return nil, ErrOpenState
		}

		// 2. 包装流以支持消息级熔断 (BreakOnMessage)
		// 即使 breakOnMessage 为 false，我们仍需要包装以支持未来可能的热更新配置(如果支持的话)，
		// 或者仅仅为了保持行为一致性。但这里我们优化一下，
		// 如果不开启 breakOnMessage，是否可以直接返回原 stream?
		// 为了支持后续可能的扩展，我们还是包装一下，但在 wrapper 内部判断。
		return &clientStreamWrapper{
			ClientStream: stream,
			cb:           cb,
			key:          key,
			cfg:          cfg,
		}, nil
	}
}

func defaultKeyFunc(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
	return cc.Target()
}

// clientStreamWrapper 包装 grpc.ClientStream 以支持消息级熔断
type clientStreamWrapper struct {
	grpc.ClientStream
	cb  *circuitBreaker
	key string
	cfg *interceptorConfig
}

func (s *clientStreamWrapper) SendMsg(m interface{}) error {
	// 如果未开启消息级熔断，直接发送
	if !s.cfg.breakOnMessage {
		return s.ClientStream.SendMsg(m)
	}

	// 使用 executeWithoutFallback 执行发送
	// 避免 fallback 导致的“假成功”
	res, err := s.cb.executeWithoutFallback(s.key, func() (interface{}, error) {
		err := s.ClientStream.SendMsg(m)

		// 判定是否计入熔断失败
		if s.cfg.failureClassifier(err) {
			return nil, err // 计为失败
		}

		// 即使 err 不为 nil (例如 EOF 或被忽略的错误)，只要不计为失败，就返回 nil
		return err, nil
	})

	if err != nil {
		return err // 熔断器打开或 execute 内部返回的错误
	}

	// 还原原始结果
	if res != nil {
		if e, ok := res.(error); ok {
			return e
		}
	}

	return nil
}

func (s *clientStreamWrapper) RecvMsg(m interface{}) error {
	// 如果未开启消息级熔断，直接接收
	if !s.cfg.breakOnMessage {
		return s.ClientStream.RecvMsg(m)
	}

	// 使用 executeWithoutFallback 执行接收
	res, err := s.cb.executeWithoutFallback(s.key, func() (interface{}, error) {
		err := s.ClientStream.RecvMsg(m)

		// 判定是否计入熔断失败
		if s.cfg.failureClassifier(err) {
			return nil, err // 计为失败
		}

		return err, nil
	})

	if err != nil {
		return err
	}

	if res != nil {
		if e, ok := res.(error); ok {
			return e
		}
	}

	return nil
}
