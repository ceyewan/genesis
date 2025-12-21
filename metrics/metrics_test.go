package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/ceyewan/genesis/clog"
)

// TestNew 测试创建 Meter 实例
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		opts    []Option
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			opts:    nil,
			wantErr: true,
		},
		{
			name: "disabled config",
			cfg: &Config{
				Enabled: false,
			},
			opts:    nil,
			wantErr: false,
		},
		{
			name: "enabled config with minimal settings",
			cfg: &Config{
				Enabled:     true,
				ServiceName: "test-service",
				Version:     "v1.0.0",
			},
			opts:    nil,
			wantErr: false,
		},
		{
			name: "enabled config with full settings",
			cfg: &Config{
				Enabled:     true,
				ServiceName: "test-service",
				Version:     "v1.0.0",
				Port:        9091,
				Path:        "/metrics",
			},
			opts:    nil,
			wantErr: false,
		},
		{
			name: "with logger option",
			cfg: &Config{
				Enabled:     true,
				ServiceName: "test-service",
				Version:     "v1.0.0",
			},
			opts: func() []Option {
				logger, _ := clog.New(&clog.Config{Level: "debug"})
				return []Option{WithLogger(logger)}
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter, err := New(tt.cfg, tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if meter == nil {
					t.Error("New() returned nil meter")
					return
				}

				// 测试 Shutdown
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if err := meter.Shutdown(ctx); err != nil {
					t.Errorf("Shutdown() error = %v", err)
				}
			}
		})
	}
}

// TestMust 测试 Must 函数
func TestMust(t *testing.T) {
	// 正常情况应该不 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Must() panicked unexpectedly: %v", r)
		}
	}()

	cfg := &Config{
		Enabled:     true,
		ServiceName: "test-service",
		Version:     "v1.0.0",
	}

	meter := Must(cfg)
	if meter == nil {
		t.Error("Must() returned nil meter")
	}

	// 清理
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	meter.Shutdown(ctx)
}

// TestMustPanic 测试 Must 在错误时 panic
func TestMustPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Must() should have panicked")
		}
	}()

	// 使用 nil config，应该导致错误并 panic
	Must(nil)
}

// TestMeterInterface 测试 Meter 接口的完整实现
func TestMeterInterface(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
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

	// 测试 Counter
	counter, err := meter.Counter("test_counter", "测试计数器")
	if err != nil {
		t.Errorf("Counter() error = %v", err)
		return
	}
	if counter == nil {
		t.Error("Counter() returned nil")
		return
	}

	// 测试 Gauge
	gauge, err := meter.Gauge("test_gauge", "测试仪表盘")
	if err != nil {
		t.Errorf("Gauge() error = %v", err)
		return
	}
	if gauge == nil {
		t.Error("Gauge() returned nil")
		return
	}

	// 测试 Histogram
	histogram, err := meter.Histogram("test_histogram", "测试直方图")
	if err != nil {
		t.Errorf("Histogram() error = %v", err)
		return
	}
	if histogram == nil {
		t.Error("Histogram() returned nil")
		return
	}

	// 测试指标操作
	counter.Inc(ctx, L("status", "success"))
	counter.Add(ctx, 5, L("method", "POST"))

	gauge.Set(ctx, 100.5, L("type", "memory"))
	gauge.Inc(ctx, L("node", "worker1"))
	gauge.Dec(ctx, L("node", "worker1"))

	histogram.Record(ctx, 0.123, L("endpoint", "/api/users"))
	histogram.Record(ctx, 0.456, L("endpoint", "/api/orders"))
}

// TestDisabledMeter 测试禁用状态下的 Meter
func TestDisabledMeter(t *testing.T) {
	cfg := &Config{
		Enabled: false,
	}

	meter, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create disabled meter: %v", err)
	}

	ctx := context.Background()

	// 所有指标创建都应该成功但返回 noop 实现
	counter, err := meter.Counter("test_counter", "测试计数器")
	if err != nil {
		t.Errorf("Counter() on disabled meter should not error: %v", err)
	}
	if counter == nil {
		t.Error("Counter() should not return nil even on disabled meter")
	}

	gauge, err := meter.Gauge("test_gauge", "测试仪表盘")
	if err != nil {
		t.Errorf("Gauge() on disabled meter should not error: %v", err)
	}

	histogram, err := meter.Histogram("test_histogram", "测试直方图")
	if err != nil {
		t.Errorf("Histogram() on disabled meter should not error: %v", err)
	}

	// noop 操作应该不会 panic
	counter.Inc(ctx, L("status", "success"))
	counter.Add(ctx, 5, L("method", "POST"))
	gauge.Set(ctx, 100.5, L("type", "memory"))
	gauge.Inc(ctx, L("node", "worker1"))
	gauge.Dec(ctx, L("node", "worker1"))
	histogram.Record(ctx, 0.123, L("endpoint", "/api/users"))

	// Shutdown 应该成功
	if err := meter.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() on disabled meter should not error: %v", err)
	}
}

// TestMetricOptions 测试指标选项
func TestMetricOptions(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
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

	// 测试带单位的指标
	histogram, err := meter.Histogram(
		"request_duration_seconds",
		"请求耗时",
		WithUnit("seconds"),
	)
	if err != nil {
		t.Errorf("Histogram() with unit error = %v", err)
		return
	}

	// 验证指标可以正常使用
	histogram.Record(ctx, 0.123, L("endpoint", "/api/users"))

	// 测试带单位的 Counter
	counter, err := meter.Counter(
		"bytes_total",
		"字节总数",
		WithUnit("bytes"),
	)
	if err != nil {
		t.Errorf("Counter() with unit error = %v", err)
		return
	}

	counter.Inc(ctx, L("type", "upload"))
}

// TestWithLogger 测试 WithLogger 选项
func TestWithLogger(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		ServiceName: "test-service",
		Version:     "v1.0.0",
	}

	logger, _ := clog.New(&clog.Config{Level: "debug"})

	// 测试带 logger 的创建
	meter, err := New(cfg, WithLogger(logger))
	if err != nil {
		t.Errorf("New() with logger error = %v", err)
		return
	}

	if meter == nil {
		t.Error("New() with logger returned nil meter")
		return
	}

	// 清理
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	meter.Shutdown(ctx)
}

// TestWithLoggerNil 测试传入 nil logger
func TestWithLoggerNil(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		ServiceName: "test-service",
		Version:     "v1.0.0",
	}

	// 传入 nil logger 应该使用默认值
	meter, err := New(cfg, WithLogger(nil))
	if err != nil {
		t.Errorf("New() with nil logger error = %v", err)
		return
	}

	if meter == nil {
		t.Error("New() with nil logger returned nil meter")
		return
	}

	// 清理
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	meter.Shutdown(ctx)
}
