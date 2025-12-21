// Package metrics 为 Genesis 框架提供统一的指标收集能力。
// 基于 OpenTelemetry 标准构建，提供简洁的 Counter、Gauge、Histogram 指标接口，
// 并内置 Prometheus HTTP 服务器用于指标暴露。
//
// 架构说明：
//   - 属于 Genesis 四层架构中的 L0（Base）层
//   - 完全扁平化设计，无 types/ 子包
//   - 基于 OpenTelemetry 标准，确保与云原生生态兼容
//   - 内置 Prometheus HTTP 服务器，支持指标自动暴露
//
// 快速开始：
//
//	cfg := &metrics.Config{
//	    Enabled:     true,
//	    ServiceName: "my-service",
//	    Version:     "v1.0.0",
//	    Port:        9090,
//	    Path:        "/metrics",
//	}
//
//	meter, err := metrics.New(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer meter.Shutdown(ctx)
//
//	// 创建指标
//	counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
//	histogram, _ := meter.Histogram("request_duration_seconds", "请求耗时（秒）")
//
// 使用示例：
//
//	// 带标签增加计数器
//	counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
//
//	// 记录直方图值
//	histogram.Record(ctx, 0.123, metrics.L("endpoint", "/api/users"))
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/ceyewan/genesis/clog"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

// ============================================================================
// 工厂函数
// ============================================================================

// New 创建一个新的 Meter 实例
// Meter 是指标的创建工厂，用于创建 Counter、Gauge、Histogram 等指标实例
//
// 如果 cfg.Enabled 为 false，将返回一个 noop Meter，所有操作都是空操作
// 如果 cfg.Enabled 为 true，将：
//  1. 创建 OpenTelemetry MeterProvider 和 Prometheus Exporter
//  2. 设置服务资源信息（ServiceName 和 Version）
//  3. 启动 Prometheus HTTP 服务器（如果 Port 和 Path 都设置）
//  4. 返回可用的 Meter 实例
//
// 参数：
//
//	cfg  - 指标系统配置，不能为 nil
//	opts - 可选配置项，目前支持 WithLogger
//
// 返回：
//
//	Meter - 指标创建工厂实例
//	error - 创建过程中的错误，如果成功则为 nil
//
// 使用示例：
//
//	cfg := &metrics.Config{
//	    Enabled:     true,
//	    ServiceName: "my-service",
//	    Version:     "v1.0.0",
//	    Port:        9090,
//	    Path:        "/metrics",
//	}
//
//	meter, err := metrics.New(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
func New(cfg *Config, opts ...Option) (Meter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if !cfg.Enabled {
		return &noopMeter{}, nil
	}

	// 解析 options
	options := &options{}
	for _, opt := range opts {
		opt(options)
	}

	// 如果没有提供 logger，使用默认值
	if options.logger == nil {
		defaultLogger, _ := clog.New(&clog.Config{
			Level:  "info",
			Format: "console",
		})
		options.logger = defaultLogger.WithNamespace("metrics")
	}

	// 创建资源
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// 创建 Prometheus Exporter
	prometheusExporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	// 创建 Meter Provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(prometheusExporter),
		sdkmetric.WithResource(res),
	)

	// 设置全局 MeterProvider
	otel.SetMeterProvider(mp)

	// 启动 Prometheus HTTP 服务器
	if cfg.Port > 0 && cfg.Path != "" {
		go func() {
			addr := fmt.Sprintf(":%d", cfg.Port)
			mux := http.NewServeMux()
			mux.Handle(cfg.Path, promhttp.Handler())
			httpServer := &http.Server{
				Addr:    addr,
				Handler: mux,
			}
			slog.Default().Info("Starting Prometheus metrics server", "addr", addr, "path", cfg.Path)
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Default().Error("Prometheus server error", "error", err)
			}
		}()
	}

	// 获取 OTel Meter
	otelMeter := mp.Meter("genesis")

	// 包装为我们的 Meter 实现
	return &meterImpl{
		meter:    otelMeter,
		provider: mp,
		config:   cfg,
	}, nil
}

// Must 创建 Meter 实例，出错时 panic
// 这是 New 的便捷版本，适用于应用程序初始化阶段
// 如果创建失败，会直接 panic 并终止程序
//
// 注意：此函数应该只在程序主函数或 init() 中使用，
// 不应该在可以被恢复的业务逻辑中使用
//
// 参数：
//
//	cfg  - 指标系统配置，不能为 nil
//	opts - 可选配置项，目前支持 WithLogger
//
// 返回：
//
//	Meter - 指标创建工厂实例，如果出错则 panic
//
// 使用示例：
//
//	meter := metrics.Must(&metrics.Config{
//	    Enabled:     true,
//	    ServiceName: "my-service",
//	    Version:     "v1.0.0",
//	})
func Must(cfg *Config, opts ...Option) Meter {
	m, err := New(cfg, opts...)
	if err != nil {
		panic(fmt.Sprintf("failed to create metrics: %v", err))
	}
	return m
}

// ============================================================================
// Meter 实现
// ============================================================================

// meterImpl 实现 Meter 接口
type meterImpl struct {
	meter    metric.Meter
	provider *sdkmetric.MeterProvider
	config   *Config
}

// Counter 创建累加器
func (m *meterImpl) Counter(name string, desc string, opts ...MetricOption) (Counter, error) {
	c, err := m.meter.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		return nil, err
	}
	return &counterImpl{c: c}, nil
}

// Gauge 创建仪表盘
func (m *meterImpl) Gauge(name string, desc string, opts ...MetricOption) (Gauge, error) {
	g, err := m.meter.Float64Gauge(name, metric.WithDescription(desc))
	if err != nil {
		return nil, err
	}
	return &gaugeImpl{
		g:      g,
		values: make(map[string]float64),
	}, nil
}

// Histogram 创建直方图
func (m *meterImpl) Histogram(name string, desc string, opts ...MetricOption) (Histogram, error) {
	options := &MetricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64HistogramOption{
		metric.WithDescription(desc),
	}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}

	h, err := m.meter.Float64Histogram(name, otelOpts...)
	if err != nil {
		return nil, err
	}
	return &histogramImpl{h: h}, nil
}

// Shutdown 关闭 Meter，刷新所有指标
func (m *meterImpl) Shutdown(ctx context.Context) error {
	return m.provider.Shutdown(ctx)
}

// ============================================================================
// Counter 实现
// ============================================================================

// counterImpl 实现 Counter 接口
type counterImpl struct {
	c metric.Int64Counter
}

func (c *counterImpl) Inc(ctx context.Context, labels ...Label) {
	c.c.Add(ctx, 1, metric.WithAttributes(toAttributes(labels)...))
}

func (c *counterImpl) Add(ctx context.Context, val float64, labels ...Label) {
	c.c.Add(ctx, int64(val), metric.WithAttributes(toAttributes(labels)...))
}

// ============================================================================
// Gauge 实现
// ============================================================================

// gaugeImpl 实现 Gauge 接口
type gaugeImpl struct {
	g      metric.Float64Gauge
	values map[string]float64
	mu     sync.RWMutex
}

func (g *gaugeImpl) Set(ctx context.Context, val float64, labels ...Label) {
	key := labelKey(labels)
	g.mu.Lock()
	g.values[key] = val
	g.mu.Unlock()
	g.g.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

func (g *gaugeImpl) Inc(ctx context.Context, labels ...Label) {
	key := labelKey(labels)
	g.mu.Lock()
	g.values[key]++
	val := g.values[key]
	g.mu.Unlock()
	g.g.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

func (g *gaugeImpl) Dec(ctx context.Context, labels ...Label) {
	key := labelKey(labels)
	g.mu.Lock()
	g.values[key]--
	val := g.values[key]
	g.mu.Unlock()
	g.g.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

// ============================================================================
// Histogram 实现
// ============================================================================

// histogramImpl 实现 Histogram 接口
type histogramImpl struct {
	h metric.Float64Histogram
}

func (h *histogramImpl) Record(ctx context.Context, val float64, labels ...Label) {
	h.h.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

// ============================================================================
// noop 实现（当 Metrics 禁用时使用）
// ============================================================================

type noopMeter struct{}

func (n *noopMeter) Counter(name string, desc string, opts ...MetricOption) (Counter, error) {
	return &noopCounter{}, nil
}

func (n *noopMeter) Gauge(name string, desc string, opts ...MetricOption) (Gauge, error) {
	return &noopGauge{}, nil
}

func (n *noopMeter) Histogram(name string, desc string, opts ...MetricOption) (Histogram, error) {
	return &noopHistogram{}, nil
}

func (n *noopMeter) Shutdown(ctx context.Context) error {
	return nil
}

type noopCounter struct{}

func (n *noopCounter) Inc(ctx context.Context, labels ...Label)              {}
func (n *noopCounter) Add(ctx context.Context, val float64, labels ...Label) {}

type noopGauge struct{}

func (n *noopGauge) Set(ctx context.Context, val float64, labels ...Label) {}
func (n *noopGauge) Inc(ctx context.Context, labels ...Label)              {}
func (n *noopGauge) Dec(ctx context.Context, labels ...Label)              {}

type noopHistogram struct{}

func (n *noopHistogram) Record(ctx context.Context, val float64, labels ...Label) {}

// ============================================================================
// 辅助函数
// ============================================================================

func toAttributes(labels []Label) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, len(labels))
	for i, l := range labels {
		attrs[i] = attribute.String(l.Key, l.Value)
	}
	return attrs
}

// labelKey 根据标签生成唯一的键
func labelKey(labels []Label) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, len(labels))
	for i, l := range labels {
		parts[i] = l.Key + "=" + l.Value
	}
	return strings.Join(parts, "|")
}
