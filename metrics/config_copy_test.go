package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithBucketsCopiesInput(t *testing.T) {
	buckets := []float64{0.1, 0.5, 1}
	var options metricOptions

	WithBuckets(buckets)(&options)
	buckets[0] = 99

	require.Equal(t, []float64{0.1, 0.5, 1}, options.Buckets)
}

func TestDefaultServerMetricsConfigsOwnBucketSlices(t *testing.T) {
	httpFirst := DefaultHTTPServerMetricsConfig("first")
	httpSecond := DefaultHTTPServerMetricsConfig("second")
	httpFirst.DurationBuckets[0] = 99
	require.NotEqual(t, httpFirst.DurationBuckets[0], httpSecond.DurationBuckets[0])

	grpcFirst := DefaultGRPCServerMetricsConfig("first")
	grpcSecond := DefaultGRPCServerMetricsConfig("second")
	grpcFirst.DurationBuckets[0] = 99
	require.NotEqual(t, grpcFirst.DurationBuckets[0], grpcSecond.DurationBuckets[0])
}
