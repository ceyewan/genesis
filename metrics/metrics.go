// Package metrics 提供 OpenTelemetry 指标收集，内置 Prometheus HTTP 服务器。
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

// Discard 创建空操作的 Meter
func Discard() Meter {
	return &noopMeter{}
}

// New 创建 Meter 实例
func New(cfg *Config) (Meter, error) {
	if cfg == nil {
		return nil, xerrors.New("config is required")
	}

	logger := defaultLogger()

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.Version),
		),
	)
	if err != nil {
		return nil, xerrors.Wrap(err, "create resource")
	}

	prometheusExporter, err := prometheus.New()
	if err != nil {
		return nil, xerrors.Wrap(err, "create prometheus exporter")
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(prometheusExporter),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	var httpServer *http.Server
	if cfg.Port > 0 && cfg.Path != "" {
		addr := fmt.Sprintf(":%d", cfg.Port)
		mux := http.NewServeMux()
		mux.Handle(cfg.Path, promhttp.Handler())
		httpServer = &http.Server{Addr: addr, Handler: mux}
		go func() {
			logger.Info("metrics server started",
				clog.String("addr", addr),
				clog.String("path", cfg.Path))
			if err := httpServer.ListenAndServe(); err != nil && !xerrors.Is(err, http.ErrServerClosed) {
				logger.Error("metrics server error", clog.Error(err))
			}
		}()
	}

	if cfg.EnableRuntime {
		if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
			logger.Error("runtime metrics start failed", clog.Error(err))
		}
	}

	otelMeter := mp.Meter("genesis")
	return &meterImpl{
		meter:      otelMeter,
		provider:   mp,
		config:     cfg,
		httpServer: httpServer,
		logger:     logger,
	}, nil
}

type meterImpl struct {
	meter      metric.Meter
	provider   *sdkmetric.MeterProvider
	config     *Config
	httpServer *http.Server
	logger     clog.Logger
}

func (m *meterImpl) Counter(name string, desc string, opts ...MetricOption) (Counter, error) {
	options := &metricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64CounterOption{metric.WithDescription(desc)}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}

	c, err := m.meter.Float64Counter(name, otelOpts...)
	if err != nil {
		return nil, err
	}
	return &counterImpl{c: c}, nil
}

func (m *meterImpl) Gauge(name string, desc string, opts ...MetricOption) (Gauge, error) {
	options := &metricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64GaugeOption{metric.WithDescription(desc)}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}

	g, err := m.meter.Float64Gauge(name, otelOpts...)
	if err != nil {
		return nil, err
	}
	return &gaugeImpl{g: g, values: make(map[string]float64)}, nil
}

func (m *meterImpl) Histogram(name string, desc string, opts ...MetricOption) (Histogram, error) {
	options := &metricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64HistogramOption{metric.WithDescription(desc)}
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

func (m *meterImpl) Shutdown(ctx context.Context) error {
	var serverErr error
	if m.httpServer != nil {
		if err := m.httpServer.Shutdown(ctx); err != nil && !xerrors.Is(err, http.ErrServerClosed) {
			serverErr = xerrors.Wrap(err, "shutdown server")
		}
	}
	providerErr := m.provider.Shutdown(ctx)
	if providerErr != nil {
		providerErr = xerrors.Wrap(providerErr, "shutdown provider")
	}
	return xerrors.Combine(serverErr, providerErr)
}

type counterImpl struct {
	c metric.Float64Counter
}

func (c *counterImpl) Inc(ctx context.Context, labels ...Label) {
	c.c.Add(ctx, 1, metric.WithAttributes(toAttributes(labels)...))
}

func (c *counterImpl) Add(ctx context.Context, val float64, labels ...Label) {
	c.c.Add(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

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

type histogramImpl struct {
	h metric.Float64Histogram
}

func (h *histogramImpl) Record(ctx context.Context, val float64, labels ...Label) {
	h.h.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

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
	logger, err := clog.New(&clog.Config{Level: "info", Format: "console"})
	if err != nil {
		return clog.Discard()
	}
	return logger.WithNamespace("metrics")
}
