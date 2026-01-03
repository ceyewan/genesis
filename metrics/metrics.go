// Package metrics 为 Genesis 框架提供统一的指标收集能力。
// 基于 OpenTelemetry 标准构建，提供 Counter、Gauge、Histogram 指标接口，
// 并内置 Prometheus HTTP 服务器用于指标暴露。
//
// 快速开始：
//
//	meter, err := metrics.New(&metrics.Config{
//	    ServiceName: "my-service",
//	    Version:     "v1.0.0",
//	    Port:        9090,
//	    Path:        "/metrics",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer meter.Shutdown(ctx)
//
//	counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
//	counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
//
// 如需禁用指标收集，使用 Discard()：
//
//	meter := metrics.Discard()
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
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

// Discard 创建一个 noop Meter 实例，所有操作都是空操作
func Discard() Meter {
	return &noopMeter{}
}

// New 创建一个新的 Meter 实例
func New(cfg *Config) (Meter, error) {
	if cfg == nil {
		return nil, xerrors.Wrap(xerrors.ErrInvalidInput, "config is required")
	}

	logger := defaultLogger()

	// 创建资源
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Version),
		),
	)
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create resource")
	}

	// 创建 Prometheus Exporter
	prometheusExporter, err := prometheus.New()
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create prometheus exporter")
	}

	// 创建 Meter Provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(prometheusExporter),
		sdkmetric.WithResource(res),
	)

	// 设置全局 MeterProvider
	otel.SetMeterProvider(mp)

	// 启动 Prometheus HTTP 服务器
	var httpServer *http.Server
	if cfg.Port > 0 && cfg.Path != "" {
		addr := fmt.Sprintf(":%d", cfg.Port)
		mux := http.NewServeMux()
		mux.Handle(cfg.Path, promhttp.Handler())
		httpServer = &http.Server{
			Addr:    addr,
			Handler: mux,
		}
		go func() {
			logger.Info("Starting Prometheus metrics server",
				clog.String("addr", addr),
				clog.String("path", cfg.Path),
			)
			if err := httpServer.ListenAndServe(); err != nil && !xerrors.Is(err, http.ErrServerClosed) {
				logger.Error("Prometheus server error", clog.Error(err))
			}
		}()
	}

	// 启动 Runtime 监控
	if cfg.EnableRuntime {
		if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
			// Runtime 监控启动失败不应导致服务崩溃，降级为记录日志
			logger.Error("Failed to start runtime metrics", clog.Error(err))
		} else {
			logger.Info("Runtime metrics collection started")
		}
	}

	// 获取 OTel Meter
	otelMeter := mp.Meter("genesis")

	// 包装为我们的 Meter 实现
	return &meterImpl{
		meter:      otelMeter,
		provider:   mp,
		config:     cfg,
		httpServer: httpServer,
		logger:     logger,
	}, nil
}

// ============================================================================
// Meter 实现
// ============================================================================

// meterImpl 实现 Meter 接口
type meterImpl struct {
	meter      metric.Meter
	provider   *sdkmetric.MeterProvider
	config     *Config
	httpServer *http.Server
	logger     clog.Logger
}

// Counter 创建累加器
func (m *meterImpl) Counter(name string, desc string, opts ...MetricOption) (Counter, error) {
	options := &metricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64CounterOption{
		metric.WithDescription(desc),
	}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}

	c, err := m.meter.Float64Counter(name, otelOpts...)
	if err != nil {
		return nil, err
	}
	return &counterImpl{c: c}, nil
}

// Gauge 创建仪表盘
func (m *meterImpl) Gauge(name string, desc string, opts ...MetricOption) (Gauge, error) {
	options := &metricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64GaugeOption{
		metric.WithDescription(desc),
	}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}

	g, err := m.meter.Float64Gauge(name, otelOpts...)
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
	options := &metricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64HistogramOption{
		metric.WithDescription(desc),
	}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}
	if len(options.Buckets) > 0 {
		otelOpts = append(otelOpts, metric.WithExplicitBucketBoundaries(options.Buckets...))
	}

	h, err := m.meter.Float64Histogram(name, otelOpts...)
	if err != nil {
		return nil, err
	}
	return &histogramImpl{h: h}, nil
}

// Shutdown 关闭 Meter，刷新所有指标
func (m *meterImpl) Shutdown(ctx context.Context) error {
	var serverErr error
	if m.httpServer != nil {
		if err := m.httpServer.Shutdown(ctx); err != nil && !xerrors.Is(err, http.ErrServerClosed) {
			m.logger.Error("Failed to shutdown metrics server", clog.Error(err))
			serverErr = xerrors.Wrap(err, "shutdown metrics server")
		}
	}

	providerErr := m.provider.Shutdown(ctx)
	if providerErr != nil {
		providerErr = xerrors.Wrap(providerErr, "shutdown meter provider")
	}

	return xerrors.Combine(serverErr, providerErr)
}

// ============================================================================
// Counter 实现
// ============================================================================

// counterImpl 实现 Counter 接口
type counterImpl struct {
	c metric.Float64Counter
}

func (c *counterImpl) Inc(ctx context.Context, labels ...Label) {
	c.c.Add(ctx, 1, metric.WithAttributes(toAttributes(labels)...))
}

func (c *counterImpl) Add(ctx context.Context, val float64, labels ...Label) {
	c.c.Add(ctx, val, metric.WithAttributes(toAttributes(labels)...))
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

	normalized := make([]Label, len(labels))
	copy(normalized, labels)
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Key == normalized[j].Key {
			return normalized[i].Value < normalized[j].Value
		}
		return normalized[i].Key < normalized[j].Key
	})

	var b strings.Builder
	for i, l := range normalized {
		if i > 0 {
			b.WriteByte('|')
		}
		writeLabelPart(&b, l.Key)
		b.WriteByte('=')
		writeLabelPart(&b, l.Value)
	}
	return b.String()
}

func writeLabelPart(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
}

func defaultLogger() clog.Logger {
	logger, err := clog.New(&clog.Config{
		Level:  "info",
		Format: "console",
	})
	if err != nil {
		return clog.Discard()
	}
	return logger.WithNamespace("metrics")
}
