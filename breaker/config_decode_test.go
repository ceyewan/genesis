package breaker_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/breaker"
	genesisconfig "github.com/ceyewan/genesis/config"
)

func TestConfigDecodesNestedSnakeCaseYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
governance:
  breaker:
    max_keys: 77
    max_requests: 4
    interval: 3m
    timeout: 19s
    failure_ratio: 0.75
    minimum_requests: 12
`), 0o600))

	loader, err := genesisconfig.New(&genesisconfig.Config{
		Paths:     []string{dir},
		EnvPrefix: "BREAKER_CONFIG_CONTRACT",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, loader.Close()) })
	require.NoError(t, loader.Load(context.Background()))

	var got breaker.Config
	require.NoError(t, loader.UnmarshalKey("governance.breaker", &got))
	require.Equal(t, breaker.Config{
		MaxKeys:         77,
		MaxRequests:     4,
		Interval:        3 * time.Minute,
		Timeout:         19 * time.Second,
		FailureRatio:    0.75,
		MinimumRequests: 12,
	}, got)
}
