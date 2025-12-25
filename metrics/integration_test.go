package metrics

import (
	"context"
	"testing"
	"time"
)

// TestPrometheusIntegration 测试 Prometheus 集成
func TestPrometheusIntegration(t *testing.T) {
	// 使用测试端口避免冲突
	cfg := &Config{
		ServiceName: "test-service",
		Version:     "v1.0.0",
		Port:        0, // 让系统选择可用端口
		Path:        "/metrics",
	}

	meter, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create meter: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		meter.Shutdown(ctx)
	}()

	ctx := context.Background()

	// 创建指标
	counter, err := meter.Counter("http_requests_total", "HTTP 请求总数")
	if err != nil {
		t.Fatalf("Failed to create counter: %v", err)
	}

	gauge, err := meter.Gauge("memory_usage_bytes", "内存使用字节数")
	if err != nil {
		t.Fatalf("Failed to create gauge: %v", err)
	}

	histogram, err := meter.Histogram(
		"request_duration_seconds",
		"请求耗时（秒）",
		WithUnit("seconds"),
	)
	if err != nil {
		t.Fatalf("Failed to create histogram: %v", err)
	}

	// 记录一些数据
	counter.Inc(ctx, L("method", "GET"), L("status", "200"))
	counter.Add(ctx, 5, L("method", "POST"), L("status", "201"))

	gauge.Set(ctx, 1024*1024*100, L("type", "heap"))
	gauge.Inc(ctx, L("node", "worker1"))
	gauge.Dec(ctx, L("node", "worker2"))

	histogram.Record(ctx, 0.123, L("endpoint", "/api/users"))
	histogram.Record(ctx, 0.456, L("endpoint", "/api/orders"))
	histogram.Record(ctx, 0.789, L("endpoint", "/api/products"))

	// 等待一下让指标注册完成
	time.Sleep(100 * time.Millisecond)
}

// TestConcurrentMetricOperations 测试并发指标操作
func TestConcurrentMetricOperations(t *testing.T) {
	cfg := &Config{
		ServiceName: "test-service",
		Version:     "v1.0.0",
	}

	meter, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create meter: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		meter.Shutdown(ctx)
	}()

	ctx := context.Background()

	// 创建指标
	counter, err := meter.Counter("concurrent_counter", "并发测试计数器")
	if err != nil {
		t.Fatalf("Failed to create counter: %v", err)
	}

	gauge, err := meter.Gauge("concurrent_gauge", "并发测试仪表盘")
	if err != nil {
		t.Fatalf("Failed to create gauge: %v", err)
	}

	histogram, err := meter.Histogram("concurrent_histogram", "并发测试直方图")
	if err != nil {
		t.Fatalf("Failed to create histogram: %v", err)
	}

	// 并发操作数量
	const numGoroutines = 10
	const numOperations = 100

	// 启动多个 goroutine 并发操作指标
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				// 操作计数器
				counter.Inc(ctx, L("goroutine", string(rune('A'+id))))
				counter.Add(ctx, 1, L("operation", "batch"))

				// 操作仪表盘
				gauge.Set(ctx, float64(j), L("goroutine", string(rune('A'+id))))
				gauge.Inc(ctx, L("type", "increment"))

				// 操作直方图
				histogram.Record(ctx, float64(j)*0.01, L("goroutine", string(rune('A'+id))))
			}
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestMetricsWithDifferentLabels 测试不同标签组合的指标
func TestMetricsWithDifferentLabels(t *testing.T) {
	cfg := &Config{
		ServiceName: "test-service",
		Version:     "v1.0.0",
	}

	meter, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create meter: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		meter.Shutdown(ctx)
	}()

	ctx := context.Background()

	counter, err := meter.Counter("http_requests_total", "HTTP 请求总数")
	if err != nil {
		t.Fatalf("Failed to create counter: %v", err)
	}

	// 测试不同的标签组合
	testCases := []struct {
		name   string
		labels []Label
	}{
		{
			name:   "GET success",
			labels: []Label{L("method", "GET"), L("status", "200")},
		},
		{
			name:   "POST created",
			labels: []Label{L("method", "POST"), L("status", "201")},
		},
		{
			name:   "GET not found",
			labels: []Label{L("method", "GET"), L("status", "404")},
		},
		{
			name:   "POST error",
			labels: []Label{L("method", "POST"), L("status", "500"), L("error_type", "timeout")},
		},
		{
			name:   "single label",
			labels: []Label{L("service", "auth")},
		},
		{
			name:   "no labels",
			labels: []Label{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 多次操作以确保稳定性
			for i := 0; i < 3; i++ {
				counter.Inc(ctx, tc.labels...)
				counter.Add(ctx, 2, tc.labels...)
			}
		})
	}
}

// TestMeterShutdown 测试 Meter 关闭
func TestMeterShutdown(t *testing.T) {
	cfg := &Config{
		ServiceName: "test-service",
		Version:     "v1.0.0",
	}

	meter, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create meter: %v", err)
	}

	// 创建一些指标
	_, err = meter.Counter("test_counter", "测试计数器")
	if err != nil {
		t.Fatalf("Failed to create counter: %v", err)
	}

	// 正常关闭
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := meter.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// 再次关闭可能会有错误（因为 reader 已经 shutdown），这是正常的
	// OpenTelemetry 的 Shutdown 只能调用一次
	if err := meter.Shutdown(shutdownCtx); err != nil {
		// 预期的错误，不需要报错
		t.Logf("Second Shutdown() expected error: %v", err)
	}
}

// TestDiscardIntegration 测试 Discard 的集成
func TestDiscardIntegration(t *testing.T) {
	meter := Discard()

	ctx := context.Background()

	// 创建指标应该成功
	counter, err := meter.Counter("test_counter", "测试计数器")
	if err != nil {
		t.Fatalf("Failed to create counter on discard meter: %v", err)
	}

	// 操作指标应该不会 panic
	for i := 0; i < 100; i++ {
		counter.Inc(ctx, L("test", "value"))
		counter.Add(ctx, 1.5, L("batch", "true"))
	}

	// 关闭应该成功
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := meter.Shutdown(shutdownCtx); err != nil {
		t.Errorf("Shutdown() on discard meter should not error: %v", err)
	}
}
