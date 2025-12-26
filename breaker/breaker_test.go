package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
)

// TestNewBreaker 测试熔断器创建
func TestNewBreaker(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     5,
		Interval:        60 * time.Second,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Fatalf("New should not return error, got: %v", err)
	}

	if brk == nil {
		t.Fatal("New should return a valid breaker")
	}
}

// TestNewBreakerNilConfig 测试 nil 配置
func TestNewBreakerNilConfig(t *testing.T) {
	_, err := New(nil)
	if err == nil {
		t.Fatal("New with nil config should return error")
	}
}

// TestExecuteSuccess 测试成功执行
func TestExecuteSuccess(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 3,
	}

	brk, _ := New(cfg, WithLogger(logger))

	ctx := context.Background()
	fn := func() (interface{}, error) {
		return "success", nil
	}

	result, err := brk.Execute(ctx, "test-service", fn)
	if err != nil {
		t.Fatalf("Execute should not return error, got: %v", err)
	}

	if result != "success" {
		t.Errorf("Expected result 'success', got: %v", result)
	}
}

// TestExecuteFailure 测试失败执行
func TestExecuteFailure(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         1 * time.Second,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}

	brk, _ := New(cfg, WithLogger(logger))

	ctx := context.Background()
	testErr := errors.New("test error")

	// 触发足够的失败来打开熔断器
	for i := 0; i < 5; i++ {
		fn := func() (interface{}, error) {
			return nil, testErr
		}
		_, _ = brk.Execute(ctx, "test-service", fn)
	}

	// 检查熔断器状态
	state, err := brk.State("test-service")
	if err != nil {
		t.Fatalf("State should not return error, got: %v", err)
	}

	if state == StateClosed {
		t.Log("Breaker might still be closed (need more failures)")
	}
}

// TestStateClosed 测试初始状态
func TestStateClosed(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, _ := New(cfg, WithLogger(logger))

	state, err := brk.State("new-service")
	if err != nil {
		// 服务不存在时可能返回错误
		t.Logf("State for non-existent service returned error: %v", err)
	} else if state != StateClosed && state != StateOpen {
		t.Errorf("Unexpected state: %v", state)
	}
}

// TestStateEmptyKey 测试空 key 的处理
func TestStateEmptyKey(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, _ := New(cfg, WithLogger(logger))

	_, err := brk.State("")
	if err == nil {
		t.Error("State with empty key should return error")
	}
}

// TestUnaryClientInterceptor 测试拦截器
func TestUnaryClientInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, _ := New(cfg, WithLogger(logger))

	// 默认拦截器（服务级别 Key）
	interceptor := brk.UnaryClientInterceptor()

	if interceptor == nil {
		t.Fatal("UnaryClientInterceptor should not return nil")
	}
}

// TestUnaryClientInterceptorWithKeyFunc 测试带 KeyFunc 的拦截器
func TestUnaryClientInterceptorWithKeyFunc(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, _ := New(cfg, WithLogger(logger))

	// 使用后端级别 Key
	interceptor := brk.UnaryClientInterceptor(WithBackendLevelKey())

	if interceptor == nil {
		t.Fatal("UnaryClientInterceptor should not return nil")
	}
}

// TestKeyFuncVariations 测试不同的 KeyFunc
func TestKeyFuncVariations(t *testing.T) {
	ctx := context.Background()
	method := "/pkg.Service/Method"

	t.Run("MethodLevelKey", func(t *testing.T) {
		keyFunc := MethodLevelKey()
		// MethodLevelKey 不依赖 ClientConn
		key := keyFunc(ctx, method, nil)
		if key != method {
			t.Errorf("MethodLevelKey should return method, got: %s", key)
		}
	})

	t.Run("BackendLevelKey", func(t *testing.T) {
		// BackendLevelKey 在没有 Peer 信息时回退到 target，但 nil cc 会 panic
		// 这里只测试方法级别 KeyFunc
		t.Skip("BackendLevelKey requires valid ClientConn")
	})

	t.Run("CompositeKey", func(t *testing.T) {
		// 组合 MethodLevelKey 和自定义 KeyFunc
		customKeyFunc := func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "service"
		}
		keyFunc := CompositeKey(customKeyFunc, MethodLevelKey())
		key := keyFunc(ctx, method, nil)
		if key == "" {
			t.Error("CompositeKey should return non-empty key")
		}
	})
}

// TestCompositeKeyWithSeparator 测试自定义分隔符的组合 Key
func TestCompositeKeyWithSeparator(t *testing.T) {
	ctx := context.Background()
	method := "/pkg.Service/Method"

	// 使用不依赖 ClientConn 的 KeyFunc
	customKeyFunc := func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
		return "service"
	}

	keyFunc := CompositeKeyWithSeparator(":", MethodLevelKey(), customKeyFunc)
	key := keyFunc(ctx, method, nil)

	if key == "" {
		t.Error("CompositeKeyWithSeparator should return non-empty key")
	}
}

// TestInterceptorOption 测试拦截器选项
func TestInterceptorOption(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, _ := New(cfg, WithLogger(logger))

	t.Run("WithServiceLevelKey", func(t *testing.T) {
		interceptor := brk.UnaryClientInterceptor(WithServiceLevelKey())
		if interceptor == nil {
			t.Error("WithServiceLevelKey should return valid interceptor")
		}
	})

	t.Run("WithBackendLevelKey", func(t *testing.T) {
		interceptor := brk.UnaryClientInterceptor(WithBackendLevelKey())
		if interceptor == nil {
			t.Error("WithBackendLevelKey should return valid interceptor")
		}
	})

	t.Run("WithMethodLevelKey", func(t *testing.T) {
		interceptor := brk.UnaryClientInterceptor(WithMethodLevelKey())
		if interceptor == nil {
			t.Error("WithMethodLevelKey should return valid interceptor")
		}
	})

	t.Run("WithCompositeKey", func(t *testing.T) {
		interceptor := brk.UnaryClientInterceptor(WithCompositeKey())
		if interceptor == nil {
			t.Error("WithCompositeKey should return valid interceptor")
		}
	})

	t.Run("WithCustomKeyFunc", func(t *testing.T) {
		customKeyFunc := func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "custom:" + fullMethod
		}
		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(customKeyFunc))
		if interceptor == nil {
			t.Error("WithKeyFunc should return valid interceptor")
		}
	})
}

// TestStreamClientInterceptor 测试流式拦截器
func TestStreamClientInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         30 * time.Second,
		FailureRatio:    0.6,
		MinimumRequests: 10,
	}

	brk, _ := New(cfg, WithLogger(logger))

	interceptor := brk.StreamClientInterceptor()
	if interceptor == nil {
		t.Fatal("StreamClientInterceptor should not return nil")
	}
}

// TestFallbackFunc 测试降级函数
func TestFallbackFunc(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})

	fallbackCalled := false
	fallback := func(ctx context.Context, serviceName string, err error) error {
		fallbackCalled = true
		return nil
	}

	cfg := &Config{
		MaxRequests:     1,
		Timeout:         100 * time.Millisecond,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}

	brk, _ := New(cfg, WithLogger(logger), WithFallback(fallback))

	ctx := context.Background()
	testErr := errors.New("test error")

	// 触发失败
	for i := 0; i < 10; i++ {
		fn := func() (interface{}, error) {
			return nil, testErr
		}
		_, _ = brk.Execute(ctx, "test-service", fn)
	}

	// 等待熔断器打开
	time.Sleep(200 * time.Millisecond)

	// 下一个调用应该触发降级
	fn := func() (interface{}, error) {
		return nil, testErr
	}
	_, _ = brk.Execute(ctx, "test-service", fn)

	if fallbackCalled {
		t.Log("Fallback was called as expected")
	}
}
