package metrics

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// TestNew 测试创建 Meter 实例
func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "minimal config",
			cfg: &Config{
				ServiceName: "test-service",
				Version:     "v1.0.0",
			},
			wantErr: false,
		},
		{
			name: "missing service name",
			cfg: &Config{
				Version: "v1.0.0",
			},
			wantErr: true,
		},
		{
			name: "negative port",
			cfg: &Config{
				ServiceName: "test-service",
				Port:        -1,
			},
			wantErr: true,
		},
		{
			name: "invalid path",
			cfg: &Config{
				ServiceName: "test-service",
				Path:        "metrics",
			},
			wantErr: true,
		},
		{
			name: "full config",
			cfg: &Config{
				ServiceName: "test-service",
				Version:     "v1.0.0",
				Port:        9091,
				Path:        "/metrics",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meter, err := New(tt.cfg)
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

func TestNewFailsWhenMetricsPortIsInUse(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	_, err = New(&Config{
		ServiceName: "test-service",
		Version:     "v1.0.0",
		Port:        port,
		Path:        "/metrics",
	})
	if err == nil {
		t.Fatalf("New() error = nil, want listen failure")
	}
}

func TestNewInstallsGlobalMeterProvider(t *testing.T) {
	before := otel.GetMeterProvider()

	meter, err := New(&Config{
		ServiceName: "test-service",
		Version:     "v1.0.0",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	after := otel.GetMeterProvider()
	if before == after {
		t.Fatalf("global meter provider was not replaced")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := meter.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	reset := otel.GetMeterProvider()
	if reset == after {
		t.Fatalf("global meter provider was not reset after shutdown")
	}
}

// TestDiscard 测试 Discard 函数
func TestDiscard(t *testing.T) {
	meter := Discard()
	if meter == nil {
		t.Fatal("Discard() returned nil")
	}

	ctx := context.Background()

	// 所有操作都应该正常但不产生任何效果
	counter, err := meter.Counter("test", "test")
	if err != nil {
		t.Errorf("Counter() error = %v", err)
	}
	counter.Inc(ctx)

	gauge, err := meter.Gauge("test", "test")
	if err != nil {
		t.Errorf("Gauge() error = %v", err)
	}
	gauge.Set(ctx, 100)

	histogram, err := meter.Histogram("test", "test")
	if err != nil {
		t.Errorf("Histogram() error = %v", err)
	}
	histogram.Record(ctx, 0.123)

	// Shutdown 应该成功
	if err := meter.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

// TestMeterInterface 测试 Meter 接口的完整实现
func TestMeterInterface(t *testing.T) {
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

// TestMetricOptions 测试指标选项
func TestMetricOptions(t *testing.T) {
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

// TestNewDevDefaultConfig 测试开发环境默认配置
func TestNewDevDefaultConfig(t *testing.T) {
	t.Parallel()

	devCfg := NewDevDefaultConfig("test-service")

	require.NotNil(t, devCfg)
	require.Equal(t, "test-service", devCfg.ServiceName)
	require.Equal(t, "dev", devCfg.Version)
	require.Equal(t, 9090, devCfg.Port)
	require.Equal(t, "/metrics", devCfg.Path)
	require.False(t, devCfg.EnableRuntime)

	// 验证配置可以正常创建 Meter
	meter, err := New(devCfg)
	require.NoError(t, err)
	require.NotNil(t, meter)

	if meter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = meter.Shutdown(ctx)
		require.NoError(t, err)
	}
}

// TestNewProdDefaultConfig 测试生产环境默认配置
func TestNewProdDefaultConfig(t *testing.T) {
	t.Parallel()

	prodCfg := NewProdDefaultConfig("prod-service", "v1.2.3")

	require.NotNil(t, prodCfg)
	require.Equal(t, "prod-service", prodCfg.ServiceName)
	require.Equal(t, "v1.2.3", prodCfg.Version)
	require.Equal(t, 9090, prodCfg.Port)
	require.Equal(t, "/metrics", prodCfg.Path)
	require.False(t, prodCfg.EnableRuntime)

	// 验证配置可以正常创建 Meter
	// 修改端口为0以避免端口冲突
	testCfg := &Config{
		ServiceName:   prodCfg.ServiceName,
		Version:       prodCfg.Version,
		Port:          0,
		Path:          prodCfg.Path,
		EnableRuntime: prodCfg.EnableRuntime,
	}
	meter, err := New(testCfg)
	require.NoError(t, err)
	require.NotNil(t, meter)

	if meter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = meter.Shutdown(ctx)
		require.NoError(t, err)
	}
}
