package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestShutdownHonorsEachCallersContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		_ = server.Serve(listener)
	}()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	<-started

	meter := &meterImpl{
		provider:     sdkmetric.NewMeterProvider(),
		httpServer:   server,
		httpDone:     httpDone,
		shutdownDone: make(chan struct{}),
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- meter.Shutdown(context.Background()) }()

	// Wait until the first caller has started Shutdown and closed the listener.
	for {
		conn, dialErr := net.DialTimeout("tcp", listener.Addr().String(), 10*time.Millisecond)
		if dialErr != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(time.Millisecond)
	}

	callerCtx, cancel := context.WithCancel(context.Background())
	cancel()
	secondDone := make(chan error, 1)
	go func() { secondDone <- meter.Shutdown(callerCtx) }()
	select {
	case err := <-secondDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second Shutdown error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second Shutdown ignored its context")
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	<-requestDone
}

func TestFirstCanceledShutdownDoesNotAbortCleanup(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		_ = server.Serve(listener)
	}()
	go func() {
		response, requestErr := http.Get("http://" + listener.Addr().String())
		if requestErr == nil {
			_ = response.Body.Close()
		}
	}()
	<-started
	meter := &meterImpl{provider: sdkmetric.NewMeterProvider(), httpServer: server, httpDone: httpDone, shutdownDone: make(chan struct{})}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := meter.Shutdown(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("first shutdown = %v, want context.Canceled", err)
	}
	second := make(chan error, 1)
	go func() { second <- meter.Shutdown(context.Background()) }()
	select {
	case err := <-second:
		t.Fatalf("cleanup ended before request release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}

func TestNewCopiesConfigAndShutdownIsConcurrentIdempotent(t *testing.T) {
	cfg := &Config{ServiceName: "svc", Version: "v1", InstanceID: "one", Environment: "test"}
	meter, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ServiceName = "mutated"
	if got := meter.(*meterImpl).config.ServiceName; got != "svc" {
		t.Fatalf("internal service name = %q", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			errCh <- meter.Shutdown(ctx)
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Shutdown: %v", err)
		}
	}
}

// TestNew 测试创建 Meter 实例
func TestNew(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve metrics port: %v", err)
	}
	metricsPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release metrics port: %v", err)
	}

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
			name: "port without path",
			cfg: &Config{
				ServiceName: "test-service",
				Port:        9091,
			},
			wantErr: true,
		},
		{
			name: "path without port",
			cfg: &Config{
				ServiceName: "test-service",
				Path:        "/metrics",
			},
			wantErr: true,
		},
		{
			name: "full config",
			cfg: &Config{
				ServiceName:   "test-service",
				Version:       "v1.0.0",
				ListenAddress: "127.0.0.1",
				Port:          metricsPort,
				Path:          "/metrics",
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
	before := otel.GetMeterProvider()
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
	if !errors.Is(err, ErrListen) {
		t.Fatalf("New() error = %v, want ErrListen", err)
	}
	if after := otel.GetMeterProvider(); after != before {
		t.Fatal("failed New replaced the global MeterProvider")
	}
}

func TestNewUsesConfiguredListenAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	meter, err := New(&Config{
		ServiceName:   "test-service",
		ListenAddress: "127.0.0.1",
		Port:          port,
		Path:          "/metrics",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := meter.(*meterImpl).httpServer.Addr; got != net.JoinHostPort("127.0.0.1", strconv.Itoa(port)) {
		t.Fatalf("server address = %q", got)
	}
	if err := meter.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
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

// TestDefaultConfigs 测试默认配置工厂
func TestDefaultConfigs(t *testing.T) {
	// 测试开发环境默认配置
	devCfg := NewDevDefaultConfig("test-service")
	if devCfg.ServiceName != "test-service" {
		t.Errorf("ServiceName = %v, want test-service", devCfg.ServiceName)
	}
	if devCfg.Version != "dev" {
		t.Errorf("Version = %v, want dev", devCfg.Version)
	}
	if devCfg.Port != 9090 {
		t.Errorf("Port = %v, want 9090", devCfg.Port)
	}
	if devCfg.Path != "/metrics" {
		t.Errorf("Path = %v, want /metrics", devCfg.Path)
	}
	if devCfg.EnableRuntime {
		t.Errorf("EnableRuntime = %v, want false", devCfg.EnableRuntime)
	}

	// 测试生产环境默认配置
	prodCfg := NewProdDefaultConfig("prod-service", "v1.2.3")
	if prodCfg.ServiceName != "prod-service" {
		t.Errorf("ServiceName = %v, want prod-service", prodCfg.ServiceName)
	}
	if prodCfg.Version != "v1.2.3" {
		t.Errorf("Version = %v, want v1.2.3", prodCfg.Version)
	}
	if prodCfg.Port != 9090 {
		t.Errorf("Port = %v, want 9090", prodCfg.Port)
	}
	if prodCfg.Path != "/metrics" {
		t.Errorf("Path = %v, want /metrics", prodCfg.Path)
	}
	if prodCfg.EnableRuntime {
		t.Errorf("EnableRuntime = %v, want false", prodCfg.EnableRuntime)
	}

	// 验证配置可以正常创建 Meter
	meter, err := New(devCfg)
	if err != nil {
		t.Errorf("New() with dev config error = %v", err)
	}
	if meter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		meter.Shutdown(ctx)
	}
}
