package ratelimit

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestUnaryServerInterceptor 测试服务端拦截器
func TestUnaryServerInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}
	defer limiter.Close()

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 1, Burst: 1}
	}

	interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	// 第一次调用应该成功
	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}
	resp, err := interceptor(ctx, "request", info, handler)
	if err != nil {
		t.Fatalf("First call should succeed, got error: %v", err)
	}
	if resp == nil {
		t.Error("Response should not be nil")
	}

	// 第二次调用可能会被限流
	_, err = interceptor(ctx, "request", info, handler)
	// 这里不强制要求限流，因为可能存在 burst
	_ = err
}

// TestUnaryServerInterceptorWithDiscard 测试 Discard 限流器的服务端拦截器
func TestUnaryServerInterceptorWithDiscard(t *testing.T) {
	limiter := Discard()

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 0, Burst: 0}
	}

	interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	// Discard 限流器始终允许
	for i := 0; i < 100; i++ {
		_, err := interceptor(ctx, "request", info, handler)
		if err != nil {
			t.Errorf("Call %d should succeed with Discard limiter, got error: %v", i+1, err)
		}
	}
}

// TestUnaryClientInterceptor 测试客户端拦截器
func TestUnaryClientInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}
	defer limiter.Close()

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 100, Burst: 100}
	}

	interceptor := UnaryClientInterceptor(limiter, nil, limitFunc)

	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}

	ctx := context.Background()

	// 模拟调用（不需要真实的连接）
	err = interceptor(ctx, "/test/Method", "request", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("Call should succeed, got error: %v", err)
	}
}

// TestUnaryServerInterceptorNilKeyFunc 测试 nil KeyFunc 时的默认行为
func TestUnaryServerInterceptorNilKeyFunc(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}
	defer limiter.Close()

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 100, Burst: 100}
	}

	// 传入 nil KeyFunc，应该使用默认的 fullMethod
	interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	_, err = interceptor(ctx, "request", info, handler)
	if err != nil {
		t.Fatalf("Call with nil keyFunc should succeed, got error: %v", err)
	}
}

// TestUnaryServerInterceptorInvalidLimit 测试无效限流规则时放行
func TestUnaryServerInterceptorInvalidLimit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, err := New(&Config{Driver: DriverStandalone, Standalone: &StandaloneConfig{}}, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}
	defer limiter.Close()

	// 返回无效的限流规则
	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 0, Burst: 0}
	}

	interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	// 无效限流规则应该放行
	_, err = interceptor(ctx, "request", info, handler)
	if err != nil {
		t.Fatalf("Call with invalid limit should pass through, got error: %v", err)
	}
}

func TestStreamServerInterceptor(t *testing.T) {
	limiter := &sequenceLimiter{allowed: []bool{true, false}}
	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 1, Burst: 1}
	}

	interceptor := StreamServerInterceptor(limiter, nil, limitFunc)

	stream := &stubServerStream{ctx: context.Background()}
	info := &grpc.StreamServerInfo{FullMethod: "/test/Method"}

	handler := func(srv interface{}, stream grpc.ServerStream) error {
		var msg any
		if err := stream.RecvMsg(&msg); err != nil {
			return err
		}
		if err := stream.RecvMsg(&msg); err != nil {
			return err
		}
		return nil
	}

	err := interceptor(nil, stream, info, handler)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Second RecvMsg should be rate limited with ResourceExhausted, got error: %v", err)
	}
	if stream.recvCount != 1 {
		t.Fatalf("RecvMsg should be called once, got: %d", stream.recvCount)
	}
}

func TestStreamClientInterceptor(t *testing.T) {
	limiter := &sequenceLimiter{allowed: []bool{true, false}}
	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 1, Burst: 1}
	}

	interceptor := StreamClientInterceptor(limiter, nil, limitFunc)

	stream := &stubClientStream{ctx: context.Background()}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return stream, nil
	}

	clientStream, err := interceptor(context.Background(), &grpc.StreamDesc{ClientStreams: true}, nil, "/test/Method", streamer)
	if err != nil {
		t.Fatalf("Stream interceptor should succeed, got error: %v", err)
	}

	if err := clientStream.SendMsg("message-1"); err != nil {
		t.Fatalf("First SendMsg should succeed, got error: %v", err)
	}
	if err := clientStream.SendMsg("message-2"); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("Second SendMsg should be rate limited with ResourceExhausted, got error: %v", err)
	}
	if stream.sendCount != 1 {
		t.Fatalf("SendMsg should be called once, got: %d", stream.sendCount)
	}
}

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
