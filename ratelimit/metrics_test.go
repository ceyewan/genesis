package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/metrics"
)

type errorRecordingMeter struct {
	metrics.Meter
	mu    sync.Mutex
	modes []string
}

func newErrorRecordingMeter() *errorRecordingMeter {
	return &errorRecordingMeter{Meter: metrics.Discard()}
}

func (m *errorRecordingMeter) Counter(name, desc string, opts ...metrics.MetricOption) (metrics.Counter, error) {
	if name == MetricErrors {
		return errorRecordingCounter{meter: m}, nil
	}
	return m.Meter.Counter(name, desc, opts...)
}

func (m *errorRecordingMeter) recordedModes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.modes...)
}

type errorRecordingCounter struct {
	meter *errorRecordingMeter
}

func (c errorRecordingCounter) Inc(ctx context.Context, labels ...metrics.Label) {
	c.Add(ctx, 1, labels...)
}

func (c errorRecordingCounter) Add(_ context.Context, _ float64, labels ...metrics.Label) {
	for _, label := range labels {
		if label.Key == LabelMode {
			c.meter.mu.Lock()
			c.meter.modes = append(c.meter.modes, label.Value)
			c.meter.mu.Unlock()
			return
		}
	}
}

func TestStandaloneErrorMetricCountsCapacityRejection(t *testing.T) {
	meter := newErrorRecordingMeter()
	limiter, err := New(&Config{
		Driver: DriverStandalone,
		Standalone: &StandaloneConfig{
			CleanupInterval: time.Hour,
			IdleTimeout:     time.Hour,
			MaxKeys:         1,
		},
	}, WithMeter(meter))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, limiter.Close()) })

	ctx := context.Background()
	limit := Limit{Rate: 1, Burst: 1}
	_, err = limiter.Allow(ctx, "first", limit)
	require.NoError(t, err)
	_, err = limiter.Allow(ctx, "second", limit)
	require.ErrorIs(t, err, ErrKeyLimitExceeded)
	require.Equal(t, []string{"standalone"}, meter.recordedModes())
}

type closedRedisConnector struct {
	client *redis.Client
}

func (*closedRedisConnector) Connect(context.Context) error     { return nil }
func (*closedRedisConnector) Close() error                      { return nil }
func (*closedRedisConnector) HealthCheck(context.Context) error { return nil }
func (*closedRedisConnector) IsHealthy() bool                   { return false }
func (*closedRedisConnector) Name() string                      { return "closed-redis" }
func (c *closedRedisConnector) GetClient() *redis.Client        { return c.client }

func TestDistributedErrorMetricCountsRedisBackendError(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	require.NoError(t, client.Close())
	meter := newErrorRecordingMeter()
	limiter, err := New(
		&Config{Driver: DriverDistributed},
		WithRedisConnector(&closedRedisConnector{client: client}),
		WithMeter(meter),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, limiter.Close()) })

	_, err = limiter.Allow(context.Background(), "backend-error", Limit{Rate: 1, Burst: 1})
	require.Error(t, err)
	require.Equal(t, []string{"distributed"}, meter.recordedModes())
}

func TestMetricConstants(t *testing.T) {
	require.Equal(t, "ratelimit_allowed_total", MetricAllowed)
	require.Equal(t, "ratelimit_denied_total", MetricDenied)
	require.Equal(t, "ratelimit_errors_total", MetricErrors)
	require.Equal(t, "mode", LabelMode)
}
