package breaker

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// MockClientStream 用于模拟 grpc.ClientStream
type MockClientStream struct {
	grpc.ClientStream
	RecvFunc func(m interface{}) error
	SendFunc func(m interface{}) error
}

func (m *MockClientStream) RecvMsg(msg interface{}) error {
	if m.RecvFunc != nil {
		return m.RecvFunc(msg)
	}
	return nil
}

func (m *MockClientStream) SendMsg(msg interface{}) error {
	if m.SendFunc != nil {
		return m.SendFunc(msg)
	}
	return nil
}

func (m *MockClientStream) Context() context.Context {
	return context.Background()
}

func (m *MockClientStream) Header() (metadata.MD, error) { return nil, nil }
func (m *MockClientStream) Trailer() metadata.MD         { return nil }
func (m *MockClientStream) CloseSend() error             { return nil }

func TestStreamClientInterceptor_BreakOnCreate(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}
	brk, _ := New(cfg, WithLogger(logger))

	mockStream := &MockClientStream{}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return mockStream, nil
	}

	interceptor := brk.StreamClientInterceptor(
		WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-create-fail"
		}),
		WithBreakOnCreate(true),
		WithBreakOnMessage(false),
	)

	// 模拟建流失败
	failStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, errors.New("connect failed")
	}

	// 触发足够多的失败
	for i := 0; i < 3; i++ {
		_, _ = interceptor(context.Background(), nil, nil, "/test/method", failStreamer)
	}

	// 验证熔断器是否打开
	state, _ := brk.State("test-create-fail")
	// 注意：gobreaker 状态可能滞后，再次尝试应该返回 ErrOpenState
	_, err := interceptor(context.Background(), nil, nil, "/test/method", streamer)
	if !errors.Is(err, ErrOpenState) {
		t.Errorf("Expected ErrOpenState, got %v (state: %v)", err, state)
	}
}

func TestStreamClientInterceptor_BreakOnMessage(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}
	brk, _ := New(cfg, WithLogger(logger))

	mockStream := &MockClientStream{}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return mockStream, nil
	}

	interceptor := brk.StreamClientInterceptor(
		WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-message-fail"
		}),
		WithBreakOnCreate(false), // 关闭建流熔断，专注测消息熔断
		WithBreakOnMessage(true), // 开启消息熔断
	)

	stream, _ := interceptor(context.Background(), nil, nil, "/test/method", streamer)

	testErr := errors.New("send error")
	mockStream.SendFunc = func(m interface{}) error { return testErr }

	// 触发消息失败
	for i := 0; i < 3; i++ {
		_ = stream.SendMsg("msg")
	}

	// 下一次 SendMsg 应该触发熔断
	err := stream.SendMsg("msg")
	if !errors.Is(err, ErrOpenState) {
		t.Errorf("Expected ErrOpenState in SendMsg, got %v", err)
	}

	// RecvMsg 也应该熔断 (因为 breakOnMessage 是全局开关)
	err = stream.RecvMsg("msg")
	if !errors.Is(err, ErrOpenState) {
		t.Errorf("Expected ErrOpenState in RecvMsg, got %v", err)
	}
}

func TestStreamClientInterceptor_NoFallbackInMessage(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 1,
	}

	// 设置全局 fallback
	fallbackCalled := false
	fallback := func(ctx context.Context, serviceName string, err error) error {
		fallbackCalled = true
		return nil // 模拟降级成功，返回 nil error
	}

	brk, _ := New(cfg, WithLogger(logger), WithFallback(fallback))

	mockStream := &MockClientStream{}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return mockStream, nil
	}

	interceptor := brk.StreamClientInterceptor(
		WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-fallback"
		}),
		WithBreakOnCreate(false),
		WithBreakOnMessage(true),
	)

	stream, _ := interceptor(context.Background(), nil, nil, "/test/method", streamer)

	mockStream.SendFunc = func(m interface{}) error { return errors.New("fail") }

	// 触发熔断
	for i := 0; i < 5; i++ {
		_ = stream.SendMsg("msg")
	}

	// 熔断后调用 SendMsg
	fallbackCalled = false // 重置
	err := stream.SendMsg("msg")

	// 预期：
	// 1. 应该返回 ErrOpenState (或者 gobreaker.ErrOpenState)
	// 2. fallback 不应该被调用 (因为 executeWithoutFallback)
	if !errors.Is(err, ErrOpenState) {
		t.Errorf("Expected ErrOpenState, got %v", err)
	}

	if fallbackCalled {
		t.Error("Fallback should NOT be called for stream messages")
	}
}

func TestStreamClientInterceptor_FailureClassifier(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 1,
	}
	brk, _ := New(cfg, WithLogger(logger))

	mockStream := &MockClientStream{}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return mockStream, nil
	}

	ignoredErr := errors.New("ignored error")
	classifier := func(err error) bool {
		return err != nil && err != io.EOF && !errors.Is(err, ignoredErr)
	}

	interceptor := brk.StreamClientInterceptor(
		WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-classifier"
		}),
		WithBreakOnCreate(false),
		WithBreakOnMessage(true),
		WithFailureClassifier(classifier),
	)

	stream, _ := interceptor(context.Background(), nil, nil, "/test/method", streamer)

	mockStream.SendFunc = func(m interface{}) error { return ignoredErr }

	// 发送多次被忽略的错误
	for i := 0; i < 10; i++ {
		_ = stream.SendMsg("msg")
	}

	// 熔断器应该仍是 Closed
	state, _ := brk.State("test-classifier")
	if state != StateClosed {
		t.Errorf("Breaker should be closed for ignored errors, but got: %v", state)
	}
}

// TestStreamClientInterceptor_BreakOnCreate_WithFallback 验证：
// 当 BreakOnCreate=true 且 Fallback 返回 nil 时，拦截器不应返回 nil stream 导致 panic，
// 而应返回错误。
func TestStreamClientInterceptor_BreakOnCreate_WithFallback(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 1,
	}

	// 配置吞掉错误的 fallback
	fallback := func(ctx context.Context, serviceName string, err error) error {
		return nil // 降级成功（但对流来说，没有流返回就是失败）
	}

	brk, _ := New(cfg, WithLogger(logger), WithFallback(fallback))

	interceptor := brk.StreamClientInterceptor(
		WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-create-fallback"
		}),
		WithBreakOnCreate(true),
	)

	// 模拟建流失败
	failStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, errors.New("connect failed")
	}

	// 触发熔断
	for i := 0; i < 5; i++ {
		_, _ = interceptor(context.Background(), nil, nil, "/test/method", failStreamer)
	}

	// 下一次调用，熔断器打开，Execute 调用 Fallback，Fallback 返回 nil
	// 此时 interceptor 内部得到的 stream 为 nil
	stream, err := interceptor(context.Background(), nil, nil, "/test/method", failStreamer)

	// 验证结果
	if stream != nil {
		t.Error("Expected nil stream when fallback is used (or wrapped stream if we handled it differently)")
	}
	if err == nil {
		t.Error("Expected error when stream creation fails/breaks, even if fallback returns nil")
	}
	if !errors.Is(err, ErrOpenState) {
		t.Errorf("Expected ErrOpenState as fallback safeguard, got: %v", err)
	}
}

// TestStreamClientInterceptor_NilClassifier 验证 WithFailureClassifier(nil) 不会导致 panic
func TestStreamClientInterceptor_NilClassifier(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	cfg := &Config{
		MaxRequests:     1,
		MinimumRequests: 1,
	}
	brk, _ := New(cfg, WithLogger(logger))

	mockStream := &MockClientStream{}
	streamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return mockStream, nil
	}

	// 显式传入 nil
	interceptor := brk.StreamClientInterceptor(
		WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-nil-classifier"
		}),
		WithBreakOnMessage(true),
		WithFailureClassifier(nil),
	)

	stream, _ := interceptor(context.Background(), nil, nil, "/test/method", streamer)

	// 发送错误，验证不会 panic，且能正常工作（回退到默认 classifier）
	mockStream.SendFunc = func(m interface{}) error { return errors.New("err") }
	_ = stream.SendMsg("msg")
}
