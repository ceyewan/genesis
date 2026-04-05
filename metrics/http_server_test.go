package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultHTTPServerMetricsConfig(t *testing.T) {
	t.Parallel()

	serviceName := "test-service"
	cfg := DefaultHTTPServerMetricsConfig(serviceName)

	require.NotNil(t, cfg, "Config should not be nil")
	require.Equal(t, serviceName, cfg.Service, "Service name should match")
}
