package cache_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/cache"
	genesisconfig "github.com/ceyewan/genesis/config"
)

func TestConfigsDecodeNestedSnakeCaseYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(`
storage:
  cache:
    distributed:
      driver: redis
      key_prefix: service-a
      serializer: msgpack
      default_ttl: 17m
    local:
      driver: otter
      max_entries: 321
      serializer: json
      default_ttl: 45s
    multi:
      local_ttl: 30s
      backfill_ttl: 2m
      fail_open_on_local_error: false
`), 0o600))

	loader, err := genesisconfig.New(&genesisconfig.Config{
		Paths:     []string{dir},
		EnvPrefix: "CACHE_CONFIG_CONTRACT",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, loader.Close()) })
	require.NoError(t, loader.Load(context.Background()))

	var got struct {
		Distributed cache.DistributedConfig `mapstructure:"distributed"`
		Local       cache.LocalConfig       `mapstructure:"local"`
		Multi       cache.MultiConfig       `mapstructure:"multi"`
	}
	require.NoError(t, loader.UnmarshalKey("storage.cache", &got))
	require.Equal(t, cache.DistributedConfig{
		Driver:     cache.DriverRedis,
		KeyPrefix:  "service-a",
		Serializer: "msgpack",
		DefaultTTL: 17 * time.Minute,
	}, got.Distributed)
	require.Equal(t, cache.LocalConfig{
		Driver:     cache.DriverOtter,
		MaxEntries: 321,
		Serializer: "json",
		DefaultTTL: 45 * time.Second,
	}, got.Local)
	require.Equal(t, 30*time.Second, got.Multi.LocalTTL)
	require.Equal(t, 2*time.Minute, got.Multi.BackfillTTL)
	require.NotNil(t, got.Multi.FailOpenOnLocalError)
	require.False(t, *got.Multi.FailOpenOnLocalError)
}
