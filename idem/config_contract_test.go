package idem_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genesisconfig "github.com/ceyewan/genesis/config"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/idem"
)

func TestPublishedConfigDecodesNestedSnakeCase(t *testing.T) {
	t.Setenv("IDEM_CONFIG_CONTRACT_BUSINESS_IDEM_WAIT_INTERVAL", "75ms")

	dir := t.TempDir()
	contents := []byte(`business:
  idem:
    driver: memory
    prefix: "orders:idem:"
    default_ttl: 2h
    lock_ttl: 45s
    wait_timeout: 3s
    wait_interval: 25ms
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), contents, 0o600))

	loader, err := genesisconfig.New(&genesisconfig.Config{
		Paths:     []string{dir},
		EnvPrefix: "IDEM_CONFIG_CONTRACT",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, loader.Close()) })
	require.NoError(t, loader.Load(context.Background()))

	var got idem.Config
	require.NoError(t, loader.UnmarshalKey("business.idem", &got))
	require.Equal(t, idem.DriverMemory, got.Driver)
	require.Equal(t, "orders:idem:", got.Prefix)
	require.Equal(t, 2*time.Hour, got.DefaultTTL)
	require.Equal(t, 45*time.Second, got.LockTTL)
	require.Equal(t, 3*time.Second, got.WaitTimeout)
	require.Equal(t, 75*time.Millisecond, got.WaitInterval)
}

func TestConstructorErrorsAreExternallyClassifiable(t *testing.T) {
	t.Run("nil config keeps the specific and broad classes", func(t *testing.T) {
		_, err := idem.New(nil)
		require.ErrorIs(t, err, idem.ErrConfigNil)
		require.ErrorIs(t, err, idem.ErrInvalidConfig)
	})

	invalidConfigs := []struct {
		name string
		cfg  *idem.Config
	}{
		{name: "default ttl", cfg: &idem.Config{Driver: idem.DriverMemory, DefaultTTL: -time.Second}},
		{name: "lock ttl", cfg: &idem.Config{Driver: idem.DriverMemory, LockTTL: -time.Second}},
		{name: "wait timeout", cfg: &idem.Config{Driver: idem.DriverMemory, WaitTimeout: -time.Second}},
		{name: "wait interval", cfg: &idem.Config{Driver: idem.DriverMemory, WaitInterval: -time.Millisecond}},
		{name: "driver", cfg: &idem.Config{Driver: "unknown"}},
	}
	for _, test := range invalidConfigs {
		t.Run("invalid "+test.name, func(t *testing.T) {
			_, err := idem.New(test.cfg)
			require.ErrorIs(t, err, idem.ErrInvalidConfig)
		})
	}

	t.Run("missing connector", func(t *testing.T) {
		_, err := idem.New(&idem.Config{Driver: idem.DriverRedis})
		require.ErrorIs(t, err, idem.ErrConnectorNil)
		require.ErrorIs(t, err, connector.ErrClientNil)
	})

	t.Run("unconnected connector", func(t *testing.T) {
		redisConnector, err := connector.NewRedis(&connector.RedisConfig{Addr: "127.0.0.1:6379"})
		require.NoError(t, err)

		_, err = idem.New(
			&idem.Config{Driver: idem.DriverRedis},
			idem.WithRedisConnector(redisConnector),
		)
		require.ErrorIs(t, err, idem.ErrConnectorNil)
		require.ErrorIs(t, err, connector.ErrClientNil)
	})
}
