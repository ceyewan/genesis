package idgen

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
)

type failingMeter struct{ metrics.Meter }

func (f failingMeter) Counter(string, string, ...metrics.MetricOption) (metrics.Counter, error) {
	return nil, xerrors.New("instrument unavailable")
}

func TestGeneratorMetricsFailure(t *testing.T) {
	t.Parallel()
	gen, err := NewGenerator(&GeneratorConfig{WorkerID: 1}, WithMeter(failingMeter{metrics.Discard()}))
	require.NoError(t, err)
	require.NotPanics(t, func() { value, err := gen.Next(); require.NoError(t, err); require.Positive(t, value) })
}

func TestSequencerMetricsFailure(t *testing.T) {
	t.Parallel()
	conn := testkit.NewRedisContainerConnector(t)
	seq, err := NewSequencer(&SequencerConfig{}, WithRedisConnector(conn), WithMeter(failingMeter{metrics.Discard()}))
	require.NoError(t, err)
	require.NotPanics(t, func() {
		value, err := seq.Next(context.Background(), "metric-failure")
		require.NoError(t, err)
		require.EqualValues(t, 1, value)
		values, err := seq.NextBatch(context.Background(), "metric-failure", 2)
		require.NoError(t, err)
		require.Equal(t, []int64{2, 3}, values)
	})
}
