package trace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	serviceName := "test-service"
	cfg := DefaultConfig(serviceName)

	require.NotNil(t, cfg)
	require.Equal(t, serviceName, cfg.ServiceName)
	require.Equal(t, "localhost:4317", cfg.Endpoint)
	require.Equal(t, 1.0, cfg.Sampler)
	require.Equal(t, "batch", cfg.Batcher)
	require.True(t, cfg.Insecure)
}
