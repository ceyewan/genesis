package dlock_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	genesisconfig "github.com/ceyewan/genesis/config"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/dlock"
)

func TestPublishedConfigDecodesNestedSnakeCase(t *testing.T) {
	t.Setenv("DLOCK_CONFIG_CONTRACT_COORDINATION_DLOCK_RETRY_INTERVAL", "75ms")

	dir := t.TempDir()
	contents := []byte(`coordination:
  dlock:
    driver: redis
    prefix: "orders:lock:"
    default_ttl: 1750ms
    retry_interval: 25ms
`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), contents, 0o600))

	loader, err := genesisconfig.New(&genesisconfig.Config{
		Paths:     []string{dir},
		EnvPrefix: "DLOCK_CONFIG_CONTRACT",
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, loader.Close()) })
	require.NoError(t, loader.Load(context.Background()))

	var got dlock.Config
	require.NoError(t, loader.UnmarshalKey("coordination.dlock", &got))
	require.Equal(t, dlock.DriverRedis, got.Driver)
	require.Equal(t, "orders:lock:", got.Prefix)
	require.Equal(t, 1750*time.Millisecond, got.DefaultTTL)
	require.Equal(t, 75*time.Millisecond, got.RetryInterval)
}

func TestConstructorErrorsAreClassifiable(t *testing.T) {
	t.Run("nil config preserves specific and broad classes", func(t *testing.T) {
		_, err := dlock.New(nil)
		require.ErrorIs(t, err, dlock.ErrConfigNil)
		require.ErrorIs(t, err, dlock.ErrInvalidConfig)
	})

	t.Run("invalid driver", func(t *testing.T) {
		_, err := dlock.New(&dlock.Config{Driver: "unknown"})
		require.ErrorIs(t, err, dlock.ErrInvalidConfig)
	})

	t.Run("invalid redis ttl preserves specific and broad classes", func(t *testing.T) {
		_, err := dlock.New(&dlock.Config{
			Driver:     dlock.DriverRedis,
			DefaultTTL: time.Microsecond,
		})
		require.ErrorIs(t, err, dlock.ErrInvalidTTL)
		require.ErrorIs(t, err, dlock.ErrInvalidConfig)
	})

	for _, driver := range []dlock.DriverType{dlock.DriverRedis, dlock.DriverEtcd} {
		t.Run("missing "+string(driver)+" connector", func(t *testing.T) {
			_, err := dlock.New(&dlock.Config{Driver: driver})
			require.ErrorIs(t, err, dlock.ErrConnectorNil)
			require.True(t, errors.Is(err, connector.ErrClientNil))
		})
	}

	t.Run("unconnected redis connector", func(t *testing.T) {
		conn, err := connector.NewRedis(&connector.RedisConfig{Addr: "127.0.0.1:6379"})
		require.NoError(t, err)
		_, err = dlock.New(
			&dlock.Config{Driver: dlock.DriverRedis},
			dlock.WithRedisConnector(conn),
		)
		require.ErrorIs(t, err, dlock.ErrConnectorNil)
		require.ErrorIs(t, err, connector.ErrClientNil)
	})

	t.Run("unconnected etcd connector", func(t *testing.T) {
		conn, err := connector.NewEtcd(&connector.EtcdConfig{Endpoints: []string{"127.0.0.1:2379"}})
		require.NoError(t, err)
		_, err = dlock.New(
			&dlock.Config{Driver: dlock.DriverEtcd},
			dlock.WithEtcdConnector(conn),
		)
		require.ErrorIs(t, err, dlock.ErrConnectorNil)
		require.ErrorIs(t, err, connector.ErrClientNil)
	})
}
