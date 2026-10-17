package metrics

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewRejectsInvalidServeMuxPattern(t *testing.T) {
	_, err := New(&Config{ServiceName: "invalid-pattern-test", Port: 9090, Path: "/{"})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestGaugeAndUpDownCounterUseNativeAggregations(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	meter := &meterImpl{meter: provider.Meter("test")}
	gauge, err := meter.Gauge("temperature", "Current temperature", nil)
	require.NoError(t, err)
	counter, err := meter.UpDownCounter("inflight", "In-flight requests", WithUnit("{request}"))
	require.NoError(t, err)
	gauge.Set(t.Context(), 10, L("node", "one"))
	gauge.Set(t.Context(), 25, L("node", "one"))
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			for range 100 {
				counter.Add(t.Context(), 1, L("b", "two"), L("a", "one"))
			}
		})
	}
	wg.Wait()
	counter.Add(t.Context(), -5, L("a", "one"), L("b", "two"))
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &data))
	require.Len(t, data.ScopeMetrics, 1)
	require.Len(t, data.ScopeMetrics[0].Metrics, 2)
	for _, metric := range data.ScopeMetrics[0].Metrics {
		switch metric.Name {
		case "temperature":
			points := metric.Data.(metricdata.Gauge[float64]).DataPoints
			require.Len(t, points, 1)
			require.Equal(t, float64(25), points[0].Value)
		case "inflight":
			sum := metric.Data.(metricdata.Sum[float64])
			require.False(t, sum.IsMonotonic)
			require.Len(t, sum.DataPoints, 1)
			require.Equal(t, float64(1995), sum.DataPoints[0].Value)
		}
	}
}

func TestConfiguredCardinalityLimitProducesOverflow(t *testing.T) {
	meter, err := New(&Config{ServiceName: "cardinality-test", CardinalityLimit: 3})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, meter.Shutdown(context.Background())) })
	gauge, err := meter.Gauge("audit_cardinality_gauge", "Bounded gauge")
	require.NoError(t, err)
	counter, err := meter.UpDownCounter("audit_cardinality_inflight", "Bounded increments")
	require.NoError(t, err)
	for i := range 100 {
		gauge.Set(t.Context(), float64(i), L("key", strconv.Itoa(i)))
		counter.Add(t.Context(), 1, L("key", strconv.Itoa(i)))
	}
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	seen := 0
	for _, family := range families {
		if family.GetName() != "audit_cardinality_gauge" && family.GetName() != "audit_cardinality_inflight" {
			continue
		}
		seen++
		require.LessOrEqual(t, len(family.Metric), 3)
		var overflow bool
		var total float64
		for _, point := range family.Metric {
			for _, label := range point.Label {
				if (label.GetName() == "otel.metric.overflow" || label.GetName() == "otel_metric_overflow") && label.GetValue() == "true" {
					overflow = true
				}
			}
			if point.Gauge != nil {
				total += point.GetGauge().GetValue()
			}
		}
		require.True(t, overflow, "%s", family.String())
		if family.GetName() == "audit_cardinality_inflight" {
			require.Equal(t, float64(100), total)
		}
	}
	require.Equal(t, 2, seen)
}

func TestCardinalityDefaultsAndValidation(t *testing.T) {
	_, err := New(&Config{ServiceName: "invalid-limit", CardinalityLimit: -1})
	require.ErrorIs(t, err, ErrInvalidConfig)
	cfg := &Config{ServiceName: "default-limit"}
	meter, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, meter.Shutdown(context.Background())) })
	require.Zero(t, cfg.CardinalityLimit, "constructor must not mutate caller config")
	require.Equal(t, defaultCardinalityLimit, meter.(*meterImpl).config.CardinalityLimit)
	counter, err := Discard().UpDownCounter("discard", "No op", nil)
	require.NoError(t, err)
	counter.Add(t.Context(), -1)
}
