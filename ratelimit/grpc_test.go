package ratelimit

import (
	"context"
	"errors"
	"testing"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================
// 辅助类型
// ============================================================

// sequenceLimiter 返回预设的允许/拒绝序列
type sequenceLimiter struct {
	allowed []bool
	index   int
}

func (l *sequenceLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
	if l.index >= len(l.allowed) {
		return true, nil
	}
	allowed := l.allowed[l.index]
	l.index++
	return allowed, nil
}

func (l *sequenceLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error) {
	return l.Allow(ctx, key, limit)
}

func (l *sequenceLimiter) Wait(ctx context.Context, key string, limit Limit) error {
	return nil
}

func (l *sequenceLimiter) Close() error {
	return nil
}

// errorLimiter 始终返回错误的限流器
type errorLimiter struct {
	err error
}

func (l *errorLimiter) Allow(ctx context.Context, key string, limit Limit) (bool, error) {
	return false, l.err
}

func (l *errorLimiter) AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error) {
	return false, l.err
}

func (l *errorLimiter) Wait(ctx context.Context, key string, limit Limit) error {
	return l.err
}

func (l *errorLimiter) Close() error {
	return nil
}

// stubServerStream 模拟 gRPC 服务端流
type stubServerStream struct {
	grpc.ServerStream
	ctx       context.Context
	recvCount int
}

func (s *stubServerStream) SetHeader(metadata.MD) error  { return nil }
func (s *stubServerStream) SendHeader(metadata.MD) error { return nil }
func (s *stubServerStream) SetTrailer(metadata.MD)       {}
func (s *stubServerStream) Context() context.Context     { return s.ctx }
func (s *stubServerStream) SendMsg(m interface{}) error  { return nil }
func (s *stubServerStream) RecvMsg(m interface{}) error {
	s.recvCount++
	return nil
}

// stubClientStream 模拟 gRPC 客户端流
type stubClientStream struct {
	grpc.ClientStream
	ctx       context.Context
	sendCount int
}

func (s *stubClientStream) Header() (metadata.MD, error) { return nil, nil }
func (s *stubClientStream) Trailer() metadata.MD         { return nil }
func (s *stubClientStream) CloseSend() error             { return nil }
func (s *stubClientStream) Context() context.Context     { return s.ctx }
func (s *stubClientStream) RecvMsg(m interface{}) error  { return nil }
func (s *stubClientStream) SendMsg(m interface{}) error {
	s.sendCount++
	return nil
}

// ============================================================
// Unary Server Interceptor 测试
// ============================================================

func TestUnaryServerInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	require.NoError(t, err)
	defer limiter.Close()

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	t.Run("第一次请求应该成功", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

		ctx := context.Background()
		info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

		resp, err := interceptor(ctx, "request", info, handler)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Rate=1,Burst=1 时第二次请求应该被限流", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

		ctx := context.Background()
		info := &grpc.UnaryServerInfo{FullMethod: "/test/Method2"}

		// 第一次请求成功
		resp, err := interceptor(ctx, "request", info, handler)
		require.NoError(t, err)
		assert.NotNil(t, resp)

		// 第二次请求应该被限流
		resp, err = interceptor(ctx, "request", info, handler)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err), "应该返回 ResourceExhausted 状态码")
	})

	t.Run("自定义 KeyFunc 应该生效", func(t *testing.T) {
		customKey := "custom-key"
		keyFunc := func(ctx context.Context, fullMethod string) string {
			return customKey
		}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryServerInterceptor(limiter, keyFunc, limitFunc)

		ctx := context.Background()
		info := &grpc.UnaryServerInfo{FullMethod: "/test/Method3"}

		// 第一次请求成功
		_, err := interceptor(ctx, "request", info, handler)
		require.NoError(t, err)

		// 第二次请求被限流（使用相同 key）
		_, err = interceptor(ctx, "request", info, handler)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	})
}

func TestUnaryServerInterceptor_EdgeCases(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	t.Run("nil limiter 应该使用 Discard", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryServerInterceptor(nil, nil, limitFunc)

		ctx := context.Background()
		info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

		// 所有请求都应该成功
		for i := 0; i < 10; i++ {
			resp, err := interceptor(ctx, "request", info, handler)
			require.NoError(t, err)
			assert.NotNil(t, resp)
		}
	})

	t.Run("限流器错误时应该放行", func(t *testing.T) {
		errorLimiter := &errorLimiter{err: errors.New("limiter error")}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 10, Burst: 10}
		}
		interceptor := UnaryServerInterceptor(errorLimiter, nil, limitFunc)

		ctx := context.Background()
		info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

		// 限流器出错时应该放行
		resp, err := interceptor(ctx, "request", info, handler)
		require.NoError(t, err, "限流器错误时应该放行")
		assert.NotNil(t, resp)
	})

	t.Run("无效限流规则应该放行", func(t *testing.T) {
		limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
		require.NoError(t, err)
		defer limiter.Close()

		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 0, Burst: 0} // 无效规则
		}
		interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

		ctx := context.Background()
		info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

		// 无效限流规则应该放行
		resp, err := interceptor(ctx, "request", info, handler)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})
}

// ============================================================
// Unary Client Interceptor 测试
// ============================================================

func TestUnaryClientInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	require.NoError(t, err)
	defer limiter.Close()

	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}

	t.Run("第一次请求应该成功", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryClientInterceptor(limiter, nil, limitFunc)

		ctx := context.Background()
		err := interceptor(ctx, "/test/Method", "request", "reply", nil, invoker)
		require.NoError(t, err)
	})

	t.Run("Rate=1,Burst=1 时第二次请求应该被限流", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryClientInterceptor(limiter, nil, limitFunc)

		ctx := context.Background()

		// 第一次请求成功
		err := interceptor(ctx, "/test/Method2", "request", "reply", nil, invoker)
		require.NoError(t, err)

		// 第二次请求应该被限流
		err = interceptor(ctx, "/test/Method2", "request", "reply", nil, invoker)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("自定义 KeyFunc 应该生效", func(t *testing.T) {
		customKey := "client-custom-key"
		keyFunc := func(ctx context.Context, fullMethod string) string {
			return customKey
		}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryClientInterceptor(limiter, keyFunc, limitFunc)

		ctx := context.Background()

		// 第一次请求成功
		err := interceptor(ctx, "/test/Method3", "request", "reply", nil, invoker)
		require.NoError(t, err)

		// 第二次请求被限流
		err = interceptor(ctx, "/test/Method3", "request", "reply", nil, invoker)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("不同方法使用不同 key", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryClientInterceptor(limiter, nil, limitFunc)

		ctx := context.Background()

		// 不同方法应该独立限流
		methods := []string{"/test/MethodA", "/test/MethodB", "/test/MethodC"}
		for _, method := range methods {
			err := interceptor(ctx, method, "request", "reply", nil, invoker)
			require.NoError(t, err, "不同方法的第一次请求应该成功")
		}
	})
}

func TestUnaryClientInterceptor_EdgeCases(t *testing.T) {
	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}

	t.Run("nil limiter 应该使用 Discard", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := UnaryClientInterceptor(nil, nil, limitFunc)

		ctx := context.Background()

		// 所有请求都应该成功
		for i := 0; i < 10; i++ {
			err := interceptor(ctx, "/test/Method", "request", "reply", nil, invoker)
			require.NoError(t, err)
		}
	})

	t.Run("限流器错误时应该放行", func(t *testing.T) {
		errorLimiter := &errorLimiter{err: errors.New("limiter error")}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 10, Burst: 10}
		}
		interceptor := UnaryClientInterceptor(errorLimiter, nil, limitFunc)

		ctx := context.Background()
		err := interceptor(ctx, "/test/Method", "request", "reply", nil, invoker)
		require.NoError(t, err, "限流器错误时应该放行")
	})
}

// ============================================================
// Stream Server Interceptor 测试
// ============================================================

func TestStreamServerInterceptor(t *testing.T) {
	handler := func(srv interface{}, stream grpc.ServerStream) error {
		return nil
	}

	t.Run("流建立时被限流", func(t *testing.T) {
		limiter := &sequenceLimiter{allowed: []bool{false}}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamServerInterceptor(limiter, nil, limitFunc)

		stream := &stubServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test/StreamMethod"}

		err := interceptor(nil, stream, info, handler)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("流建立时被允许", func(t *testing.T) {
		limiter := &sequenceLimiter{allowed: []bool{true}}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamServerInterceptor(limiter, nil, limitFunc)

		stream := &stubServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test/StreamMethod"}

		handlerCalled := false
		handler := func(srv interface{}, stream grpc.ServerStream) error {
			handlerCalled = true
			return nil
		}

		err := interceptor(nil, stream, info, handler)
		require.NoError(t, err)
		assert.True(t, handlerCalled, "允许时应该调用 handler")
	})

	t.Run("Per-Stream 限流：只检查一次", func(t *testing.T) {
		// 模拟：第一次允许，后续都拒绝
		limiter := &sequenceLimiter{allowed: []bool{true, false, false}}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamServerInterceptor(limiter, nil, limitFunc)

		stream := &stubServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test/StreamMethod"}

		handler := func(srv interface{}, stream grpc.ServerStream) error {
			return nil
		}

		// 流建立时检查一次，后续不再检查
		err := interceptor(nil, stream, info, handler)
		require.NoError(t, err, "流建立时允许，后续不再检查")
	})

	t.Run("无效限流规则应该放行", func(t *testing.T) {
		limiter := Discard()
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 0, Burst: 0}
		}
		interceptor := StreamServerInterceptor(limiter, nil, limitFunc)

		stream := &stubServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test/StreamMethod"}

		handlerCalled := false
		handler := func(srv interface{}, stream grpc.ServerStream) error {
			handlerCalled = true
			return nil
		}

		err := interceptor(nil, stream, info, handler)
		require.NoError(t, err)
		assert.True(t, handlerCalled, "无效限流规则应该放行")
	})

	t.Run("nil limiter 应该使用 Discard", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamServerInterceptor(nil, nil, limitFunc)

		stream := &stubServerStream{ctx: context.Background()}
		info := &grpc.StreamServerInfo{FullMethod: "/test/StreamMethod"}

		handlerCalled := false
		handler := func(srv interface{}, stream grpc.ServerStream) error {
			handlerCalled = true
			return nil
		}

		err := interceptor(nil, stream, info, handler)
		require.NoError(t, err)
		assert.True(t, handlerCalled, "nil limiter 应该放行")
	})
}

// ============================================================
// Stream Client Interceptor 测试
// ============================================================

func TestStreamClientInterceptor(t *testing.T) {
	t.Run("流建立时被限流", func(t *testing.T) {
		limiter := &sequenceLimiter{allowed: []bool{false}}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamClientInterceptor(limiter, nil, limitFunc)

		streamerCalled := false
		streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			streamerCalled = true
			return &stubClientStream{ctx: ctx}, nil
		}

		_, err := interceptor(context.Background(), &grpc.StreamDesc{ClientStreams: true}, nil, "/test/StreamMethod", streamer)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
		assert.False(t, streamerCalled, "限流时不应该调用 streamer")
	})

	t.Run("流建立时被允许", func(t *testing.T) {
		limiter := &sequenceLimiter{allowed: []bool{true}}
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamClientInterceptor(limiter, nil, limitFunc)

		stream := &stubClientStream{ctx: context.Background()}
		streamerCalled := false
		streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			streamerCalled = true
			return stream, nil
		}

		clientStream, err := interceptor(context.Background(), &grpc.StreamDesc{ClientStreams: true}, nil, "/test/StreamMethod", streamer)
		require.NoError(t, err)
		assert.NotNil(t, clientStream)
		assert.True(t, streamerCalled, "允许时应该调用 streamer")
	})

	t.Run("nil limiter 应该使用 Discard", func(t *testing.T) {
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 1, Burst: 1}
		}
		interceptor := StreamClientInterceptor(nil, nil, limitFunc)

		streamerCalled := false
		streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			streamerCalled = true
			return &stubClientStream{ctx: ctx}, nil
		}

		_, err := interceptor(context.Background(), &grpc.StreamDesc{ClientStreams: true}, nil, "/test/StreamMethod", streamer)
		require.NoError(t, err)
		assert.True(t, streamerCalled, "nil limiter 应该放行")
	})

	t.Run("无效限流规则应该放行", func(t *testing.T) {
		limiter := Discard()
		limitFunc := func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 0, Burst: 0}
		}
		interceptor := StreamClientInterceptor(limiter, nil, limitFunc)

		streamerCalled := false
		streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			streamerCalled = true
			return &stubClientStream{ctx: ctx}, nil
		}

		_, err := interceptor(context.Background(), &grpc.StreamDesc{ClientStreams: true}, nil, "/test/StreamMethod", streamer)
		require.NoError(t, err)
		assert.True(t, streamerCalled, "无效限流规则应该放行")
	})
}

// ============================================================
// grpcLimiterConfig 测试
// ============================================================

func TestGRPCLimiterConfig(t *testing.T) {
	t.Run("newGRPCLimiterConfig 默认值", func(t *testing.T) {
		cfg := newGRPCLimiterConfig(nil, nil, nil)

		assert.NotNil(t, cfg.limiter, "nil limiter 应该被替换为 Discard")
		assert.NotNil(t, cfg.keyFunc, "nil keyFunc 应该被替换为默认函数")
		assert.NotNil(t, cfg.limitFunc, "nil limitFunc 应该被替换为默认函数")
	})

	t.Run("check 方法 - 无效限流规则放行", func(t *testing.T) {
		limiter := Discard()
		cfg := newGRPCLimiterConfig(limiter, nil, func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 0, Burst: 0}
		})

		allowed, passThrough := cfg.check(context.Background(), "/test/Method")
		assert.False(t, allowed)
		assert.True(t, passThrough, "无效限流规则应该放行")
	})

	t.Run("check 方法 - 限流器错误放行", func(t *testing.T) {
		errorLimiter := &errorLimiter{err: errors.New("limiter error")}
		cfg := newGRPCLimiterConfig(errorLimiter, nil, func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 10, Burst: 10}
		})

		allowed, passThrough := cfg.check(context.Background(), "/test/Method")
		assert.False(t, allowed)
		assert.True(t, passThrough, "限流器错误应该放行")
	})

	t.Run("check 方法 - 请求被允许", func(t *testing.T) {
		limiter := &sequenceLimiter{allowed: []bool{true}}
		cfg := newGRPCLimiterConfig(limiter, nil, func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 10, Burst: 10}
		})

		allowed, passThrough := cfg.check(context.Background(), "/test/Method")
		assert.True(t, allowed)
		assert.False(t, passThrough, "允许请求不应该放行")
	})

	t.Run("check 方法 - 请求被拒绝", func(t *testing.T) {
		limiter := &sequenceLimiter{allowed: []bool{false}}
		cfg := newGRPCLimiterConfig(limiter, nil, func(ctx context.Context, fullMethod string) Limit {
			return Limit{Rate: 10, Burst: 10}
		})

		allowed, passThrough := cfg.check(context.Background(), "/test/Method")
		assert.False(t, allowed)
		assert.False(t, passThrough, "拒绝请求不应该放行")
	})
}

// ============================================================
// 默认 KeyFunc 测试
// ============================================================

func TestDefaultGRPCKeyFunc(t *testing.T) {
	t.Run("应该返回 fullMethod", func(t *testing.T) {
		method := "/test.service/Method"
		key := defaultGRPCKeyFunc(context.Background(), method)
		assert.Equal(t, method, key)
	})
}
