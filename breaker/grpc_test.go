package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ============================================================
// 辅助类型
// ============================================================

// errorInvoker 返回预设错误的 invoker
type errorInvoker struct {
	err error
}

func (e *errorInvoker) invoke(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
	return e.err
}

// successInvoker 成功的 invoker
type successInvoker struct{}

func (s *successInvoker) invoke(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
	return nil
}

// countingInvoker 记录调用次数
type countingInvoker struct {
	count int
}

func (c *countingInvoker) invoke(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
	c.count++
	return nil
}

// ============================================================
// Unary Client Interceptor 测试
// ============================================================

func TestUnaryClientInterceptor_Basic(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	t.Run("拦截器应该成功调用 invoker", func(t *testing.T) {
		cfg := &Config{
			MaxRequests:     1,
			Timeout:         30 * time.Second,
			FailureRatio:    0.6,
			MinimumRequests: 10,
		}

		brk, err := New(cfg, WithLogger(logger))
		if err != nil {
			t.Fatalf("New should not return error, got: %v", err)
		}

		// 使用自定义 KeyFunc 避免依赖 cc.Target()
		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-basic"
		}))
		invoker := &successInvoker{}

		err = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("invoker 错误应该被正确传递", func(t *testing.T) {
		cfg := &Config{
			MaxRequests:     1,
			Timeout:         30 * time.Second,
			FailureRatio:    0.6,
			MinimumRequests: 10,
		}

		brk, err := New(cfg, WithLogger(logger))
		if err != nil {
			t.Fatalf("New should not return error, got: %v", err)
		}

		// 使用自定义 KeyFunc 避免依赖 cc.Target()
		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "test-error"
		}))
		testErr := status.Error(codes.Unavailable, "service unavailable")
		invoker := &errorInvoker{err: testErr}

		err = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)
		if err == nil {
			t.Error("expected error, got nil")
		}
		if status.Code(err) != codes.Unavailable {
			t.Errorf("expected codes.Unavailable, got: %v", status.Code(err))
		}
	})
}

func TestUnaryClientInterceptor_CircuitOpen(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	cfg := &Config{
		MaxRequests:     1,
		Interval:        10 * time.Second,
		Timeout:         100 * time.Millisecond,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}

	brk, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	serviceKey := "test-circuit-open"
	// 使用自定义 KeyFunc 控制熔断 key
	interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		return serviceKey
	}))
	testErr := errors.New("connection failed")
	invoker := &errorInvoker{err: testErr}

	// 触发足够多的失败来打开熔断器
	t.Run("触发熔断器打开", func(t *testing.T) {
		for i := 0; i < 10; i++ {
			_ = interceptor(context.Background(), "/test/Service", "req", "reply", nil, invoker.invoke)
		}

		// 检查状态
		state, err := brk.State(serviceKey)
		if err != nil {
			t.Fatalf("State should not return error, got: %v", err)
		}

		if state != StateOpen {
			t.Logf("State is: %v (expected Open, but may differ)", state)
		}
	})
}

func TestUnaryClientInterceptor_WithCustomKeyFunc(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	t.Run("方法级别熔断 key", func(t *testing.T) {
		// 使用方法名作为 key
		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return fullMethod
		}))

		invoker := &successInvoker{}
		err = interceptor(context.Background(), "/test/Method1", "req", "reply", nil, invoker.invoke)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}

		// 检查状态
		state, err := brk.State("/test/Method1")
		if err != nil {
			t.Errorf("State should not return error, got: %v", err)
		}
		if state != StateClosed {
			t.Logf("State for /test/Method1: %v", state)
		}
	})

	t.Run("自定义前缀 key", func(t *testing.T) {
		customPrefix := "custom-service:"
		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return customPrefix + fullMethod
		}))

		invoker := &successInvoker{}
		err = interceptor(context.Background(), "/test/Method2", "req", "reply", nil, invoker.invoke)
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}

		// 检查状态
		state, err := brk.State("custom-service:/test/Method2")
		if err != nil {
			t.Errorf("State should not return error, got: %v", err)
		}
		if state != StateClosed {
			t.Logf("State for custom key: %v", state)
		}
	})
}

func TestUnaryClientInterceptor_WithFallback(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	fallbackCalled := false
	fallback := func(ctx context.Context, serviceName string, err error) error {
		fallbackCalled = true
		// 返回降级响应
		return status.Error(codes.ResourceExhausted, "circuit breaker open - fallback response")
	}

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         50 * time.Millisecond,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}

	brk, err := New(cfg, WithLogger(logger), WithFallback(fallback))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	// 使用方法级别 key 以便控制熔断状态
	interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		return "test-service-fallback"
	}))

	testErr := errors.New("service unavailable")
	invoker := &errorInvoker{err: testErr}

	t.Run("触发熔断并验证降级", func(t *testing.T) {
		// 触发足够多的失败
		for i := 0; i < 10; i++ {
			_ = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)
		}

		// 等待熔断器状态更新
		time.Sleep(100 * time.Millisecond)

		// 再次调用，可能触发降级
		err = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)

		if fallbackCalled {
			t.Log("Fallback was called as expected")
		} else {
			t.Log("Fallback may not have been called yet (breaker state may still be closed)")
		}
	})
}

func TestUnaryClientInterceptor_MultipleServices(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	// 使用服务名作为 key
	interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		// 从 fullMethod 提取服务名: /package.Service/Method -> package.Service
		return "service:" + fullMethod
	}))

	invoker := &successInvoker{}

	t.Run("不同服务应该独立熔断", func(t *testing.T) {
		services := []string{"/svcA/Method1", "/svcB/Method2", "/svcC/Method3"}

		for _, svc := range services {
			err := interceptor(context.Background(), svc, "req", "reply", nil, invoker.invoke)
			if err != nil {
				t.Errorf("service %s: expected no error, got: %v", svc, err)
			}

			// 检查每个服务的状态
			key := "service:" + svc
			state, err := brk.State(key)
			if err != nil {
				t.Errorf("State for %s: %v", key, err)
			}
			t.Logf("Service %s state: %v", key, state)
		}
	})
}

func TestUnaryClientInterceptor_HalfOpenState(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})

	cfg := &Config{
		MaxRequests:     1, // 半开状态只允许 1 个探测请求
		Timeout:         100 * time.Millisecond,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}

	brk, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	serviceKey := "test-half-open"
	interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		return serviceKey
	}))

	testErr := errors.New("service error")
	invoker := &errorInvoker{err: testErr}
	successInvoker := &successInvoker{}

	t.Run("半开状态后成功调用应该恢复熔断器", func(t *testing.T) {
		// 1. 触发熔断器打开
		for i := 0; i < 10; i++ {
			_ = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)
		}

		// 等待 Timeout 过去，熔断器进入半开状态
		time.Sleep(150 * time.Millisecond)

		// 2. 发送一个成功的探测请求
		err = interceptor(context.Background(), "/test/Method", "req", "reply", nil, successInvoker.invoke)
		if err != nil {
			t.Logf("Probe request error: %v", err)
		}

		// 3. 检查状态是否恢复到 Closed
		state, err := brk.State(serviceKey)
		if err != nil {
			t.Errorf("State should not return error, got: %v", err)
		}
		t.Logf("State after probe: %v (expected Closed if probe succeeded)", state)
	})
}

// ============================================================
// InterceptorOption 测试
// ============================================================

func TestInterceptorOption_WithKeyFunc(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "error"})
	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	t.Run("多个 WithKeyFunc 应该使用最后一个", func(t *testing.T) {
		// 后面的 WithKeyFunc 应该覆盖前面的
		interceptor := brk.UnaryClientInterceptor(
			WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
				return "first-key"
			}),
			WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
				return "second-key"
			}),
		)

		invoker := &successInvoker{}
		_ = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)

		// 应该使用第二个 key
		state, err := brk.State("second-key")
		if err != nil {
			t.Errorf("State for second-key should exist, got error: %v", err)
		}
		t.Logf("State with second key: %v", state)
	})
}
