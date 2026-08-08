package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ceyewan/genesis/clog"
)

// ============================================================
// 辅助类型
// ============================================================

// errorInvoker 返回预设错误的 invoker
type errorInvoker struct {
	err error
}

func (e *errorInvoker) invoke(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
	return e.err
}

// successInvoker 成功的 invoker
type successInvoker struct{}

func (s *successInvoker) invoke(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
	return nil
}

func TestShouldCountGRPCFailurePreservesContextErrorChains(t *testing.T) {
	t.Parallel()

	requireNotCounted := func(t *testing.T, err error) {
		t.Helper()
		if shouldCountGRPCFailure(err) {
			t.Fatalf("wrapped caller cancellation must not count as a breaker failure: %v", err)
		}
	}

	t.Run("canceled", func(t *testing.T) {
		requireNotCounted(t, errors.Join(errors.New("request aborted"), context.Canceled))
	})
	t.Run("deadline exceeded", func(t *testing.T) {
		requireNotCounted(t, errors.Join(errors.New("request timed out"), context.DeadlineExceeded))
	})
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
		for range 2 {
			if err := interceptor(context.Background(), "/test/Service", "req", "reply", nil, invoker.invoke); !errors.Is(err, testErr) {
				t.Fatalf("interceptor error = %v, want %v", err, testErr)
			}
		}

		// 检查状态
		state, err := brk.State(serviceKey)
		if err != nil {
			t.Fatalf("State should not return error, got: %v", err)
		}

		if state != StateOpen {
			t.Fatalf("State = %v, want open", state)
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
			t.Fatalf("State for /test/Method1 = %v, want closed", state)
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
			t.Fatalf("State for custom key = %v, want closed", state)
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
		for range 2 {
			if callErr := interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke); !errors.Is(callErr, testErr) {
				t.Fatalf("interceptor error = %v, want %v", callErr, testErr)
			}
		}

		err = interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke)
		if !fallbackCalled {
			t.Fatal("fallback was not called for an open breaker")
		}
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("fallback error code = %v, want ResourceExhausted", status.Code(err))
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
			if state != StateClosed {
				t.Fatalf("Service %s state = %v, want closed", key, state)
			}
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
		for range 2 {
			if callErr := interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke); !errors.Is(callErr, testErr) {
				t.Fatalf("interceptor error = %v, want %v", callErr, testErr)
			}
		}
		state, stateErr := brk.State(serviceKey)
		if stateErr != nil || state != StateOpen {
			t.Fatalf("State before probe = %v, err = %v, want open", state, stateErr)
		}

		// 等待 Timeout 过去，熔断器进入半开状态
		time.Sleep(150 * time.Millisecond)

		// 2. 发送一个成功的探测请求
		err = interceptor(context.Background(), "/test/Method", "req", "reply", nil, successInvoker.invoke)
		if err != nil {
			t.Fatalf("Probe request error: %v", err)
		}

		// 3. 检查状态是否恢复到 Closed
		state, err = brk.State(serviceKey)
		if err != nil {
			t.Errorf("State should not return error, got: %v", err)
		}
		if state != StateClosed {
			t.Fatalf("State after probe = %v, want closed", state)
		}
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
		if err := interceptor(context.Background(), "/test/Method", "req", "reply", nil, invoker.invoke); err != nil {
			t.Fatal(err)
		}

		// 应该使用第二个 key
		internal := brk.(*circuitBreaker)
		if _, ok := internal.breakers.Load("second-key"); !ok {
			t.Fatal("last WithKeyFunc value was not used")
		}
		if _, ok := internal.breakers.Load("first-key"); ok {
			t.Fatal("earlier WithKeyFunc value was unexpectedly used")
		}
	})
}
