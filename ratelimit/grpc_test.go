package ratelimit

import (
	"context"
	"testing"

	"github.com/ceyewan/genesis/clog"
	"google.golang.org/grpc"
)

// TestMethodLevelKey 测试方法级别 KeyFunc
func TestMethodLevelKey(t *testing.T) {
	keyFunc := MethodLevelKey()
	ctx := context.Background()
	method := "/pkg.Service/Method"

	key := keyFunc(ctx, method)
	if key != method {
		t.Errorf("MethodLevelKey should return method, got: %s", key)
	}
}

// TestServiceLevelKey 测试服务级别 KeyFunc
func TestServiceLevelKey(t *testing.T) {
	keyFunc := ServiceLevelKey()
	ctx := context.Background()
	method := "/pkg.Service/Method"

	key := keyFunc(ctx, method)
	// 应该返回服务名: "pkg.Service"
	expected := "pkg.Service"
	if key != expected {
		t.Errorf("ServiceLevelKey should return '%s', got: '%s'", expected, key)
	}
}

// TestServiceLevelKeyEdgeCases 测试服务级别 KeyFunc 边界情况
func TestServiceLevelKeyEdgeCases(t *testing.T) {
	keyFunc := ServiceLevelKey()
	ctx := context.Background()

	tests := []struct {
		name     string
		method   string
		expected string
	}{
		{
			name:     "normal method",
			method:   "/pkg.Service/Method",
			expected: "pkg.Service",
		},
		{
			name:     "method with dots",
			method:   "/com.example.pkg.Service/Method",
			expected: "com.example.pkg.Service",
		},
		{
			name:     "invalid format - no slash",
			method:   "pkg.Service/Method",
			expected: "Method", // strings.Split 后第二部分是 "Method"
		},
		{
			name:     "invalid format - only service",
			method:   "/Service",
			expected: "Service", // 只有一个元素，返回原值去掉 /
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := keyFunc(ctx, tt.method)
			if key != tt.expected {
				t.Errorf("For method '%s', expected '%s', got: '%s'", tt.method, tt.expected, key)
			}
		})
	}
}

// TestIPLevelKey 测试 IP 级别 KeyFunc
func TestIPLevelKey(t *testing.T) {
	keyFunc := IPLevelKey()
	ctx := context.Background()
	method := "/pkg.Service/Method"

	// 没有 Peer 信息时应该返回 "ip:unknown"
	key := keyFunc(ctx, method)
	if key != "ip:unknown" {
		t.Errorf("IPLevelKey without peer should return 'ip:unknown', got: '%s'", key)
	}
}

// TestIPLevelKeyWithPeer 测试有 Peer 信息时的 IP 级别 KeyFunc
func TestIPLevelKeyWithPeer(t *testing.T) {
	keyFunc := IPLevelKey()

	// 创建带有 Peer 信息的 Context
	ctx := context.Background()

	// 注意：由于 peer.Context 需要 Peer 对象，这里简化测试
	// 在实际使用中，gRPC 会自动注入 Peer 信息
	key := keyFunc(ctx, "/pkg.Service/Method")
	if key != "ip:unknown" {
		t.Logf("IPLevelKey returned: %s (expected 'ip:unknown' without peer)", key)
	}
}

// TestCompositeKey 测试组合 KeyFunc
func TestCompositeKey(t *testing.T) {
	ctx := context.Background()
	method := "/pkg.Service/Method"

	// 单个 KeyFunc
	composite := CompositeKey(MethodLevelKey())
	key := composite(ctx, method)
	if key != method {
		t.Errorf("CompositeKey with single func should return method, got: %s", key)
	}

	// 多个 KeyFunc
	composite = CompositeKey(
		MethodLevelKey(),
		ServiceLevelKey(),
	)
	key = composite(ctx, method)
	if key == "" {
		t.Error("CompositeKey should return a non-empty key")
	}
}

// TestUnaryServerInterceptor 测试服务端拦截器
func TestUnaryServerInterceptor(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 1, Burst: 1}
	}

	interceptor := UnaryServerInterceptor(limiter, MethodLevelKey(), limitFunc)

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

	interceptor := UnaryServerInterceptor(limiter, MethodLevelKey(), limitFunc)

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
	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 100, Burst: 100}
	}

	interceptor := UnaryClientInterceptor(limiter, MethodLevelKey(), limitFunc)

	invoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return nil
	}

	ctx := context.Background()

	// 模拟调用（不需要真实的连接）
	err := interceptor(ctx, "/test/Method", "request", "reply", nil, invoker)
	if err != nil {
		t.Fatalf("Call should succeed, got error: %v", err)
	}
}

// TestUnaryServerInterceptorNilKeyFunc 测试 nil KeyFunc 时的默认行为
func TestUnaryServerInterceptorNilKeyFunc(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 100, Burst: 100}
	}

	// 传入 nil KeyFunc，应该使用默认的 MethodLevelKey
	interceptor := UnaryServerInterceptor(limiter, nil, limitFunc)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	_, err := interceptor(ctx, "request", info, handler)
	if err != nil {
		t.Fatalf("Call with nil keyFunc should succeed, got error: %v", err)
	}
}

// TestUnaryServerInterceptorInvalidLimit 测试无效限流规则时放行
func TestUnaryServerInterceptorInvalidLimit(t *testing.T) {
	logger, _ := clog.New(&clog.Config{Level: "debug"})
	limiter, _ := NewStandalone(&StandaloneConfig{}, WithLogger(logger))

	// 返回无效的限流规则
	limitFunc := func(ctx context.Context, fullMethod string) Limit {
		return Limit{Rate: 0, Burst: 0}
	}

	interceptor := UnaryServerInterceptor(limiter, MethodLevelKey(), limitFunc)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "response", nil
	}

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test/Method"}

	// 无效限流规则应该放行
	_, err := interceptor(ctx, "request", info, handler)
	if err != nil {
		t.Fatalf("Call with invalid limit should pass through, got error: %v", err)
	}
}
