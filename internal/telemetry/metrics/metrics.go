package metrics

import (
	"context"

	"github.com/ceyewan/genesis/pkg/telemetry/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// counter 使用 OpenTelemetry 的 Int64Counter 实现 types.Counter 接口。
type counter struct {
	c metric.Int64Counter
}

// NewCounter 创建一个新的 Counter 指标。
func NewCounter(m metric.Meter, name, desc string) (types.Counter, error) {
	c, err := m.Int64Counter(name, metric.WithDescription(desc))
	if err != nil {
		return nil, err
	}
	return &counter{c: c}, nil
}

// Inc 将计数器增加 1。
func (c *counter) Inc(ctx context.Context, labels ...types.Label) {
	c.c.Add(ctx, 1, metric.WithAttributes(toAttributes(labels)...))
}

// Add 将计数器增加给定的值。
func (c *counter) Add(ctx context.Context, val float64, labels ...types.Label) {
	c.c.Add(ctx, int64(val), metric.WithAttributes(toAttributes(labels)...))
}

// gauge 使用 OpenTelemetry 的 Float64Gauge 实现 types.Gauge 接口。
type gauge struct {
	syncG metric.Float64Gauge
}

// NewGauge 创建一个新的 Gauge 指标。
func NewGauge(m metric.Meter, name, desc string) (types.Gauge, error) {
	g, err := m.Float64Gauge(name, metric.WithDescription(desc))
	if err != nil {
		return nil, err
	}
	return &gauge{syncG: g}, nil
}

// Set 将 gauge 设置为给定的值。
func (g *gauge) Set(ctx context.Context, val float64, labels ...types.Label) {
	g.syncG.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

// Record 是 Set 的别名。
func (g *gauge) Record(ctx context.Context, val float64, labels ...types.Label) {
	g.Set(ctx, val, labels...)
}

// histogram 使用 OpenTelemetry 的 Float64Histogram 实现 types.Histogram 接口。
type histogram struct {
	h metric.Float64Histogram
}

// NewHistogram 创建一个新的 Histogram 指标。
func NewHistogram(m metric.Meter, name, desc string, opts ...types.MetricOption) (types.Histogram, error) {
	options := &types.MetricOptions{}
	for _, o := range opts {
		o(options)
	}

	otelOpts := []metric.Float64HistogramOption{
		metric.WithDescription(desc),
	}
	if options.Unit != "" {
		otelOpts = append(otelOpts, metric.WithUnit(options.Unit))
	}

	h, err := m.Float64Histogram(name, otelOpts...)
	if err != nil {
		return nil, err
	}
	return &histogram{h: h}, nil
}

// Record 在直方图中记录一个值。
func (h *histogram) Record(ctx context.Context, val float64, labels ...types.Label) {
	h.h.Record(ctx, val, metric.WithAttributes(toAttributes(labels)...))
}

func toAttributes(labels []types.Label) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, len(labels))
	for i, l := range labels {
		attrs[i] = attribute.String(l.Key, l.Value)
	}
	return attrs
}

// MeterImpl 实现 types.Meter 接口。
type MeterImpl struct {
	meter metric.Meter
}

// NewMeter 创建一个新的 MeterImpl 实例。
func NewMeter(m metric.Meter) *MeterImpl {
	return &MeterImpl{meter: m}
}

// Counter 创建一个新的 Counter 指标。
func (m *MeterImpl) Counter(name string, desc string, opts ...types.MetricOption) (types.Counter, error) {
	return NewCounter(m.meter, name, desc)
}

// Gauge 创建一个新的 Gauge 指标。
func (m *MeterImpl) Gauge(name string, desc string, opts ...types.MetricOption) (types.Gauge, error) {
	return NewGauge(m.meter, name, desc)
}

// Histogram 创建一个新的 Histogram 指标。
func (m *MeterImpl) Histogram(name string, desc string, opts ...types.MetricOption) (types.Histogram, error) {
	return NewHistogram(m.meter, name, desc, opts...)
}
