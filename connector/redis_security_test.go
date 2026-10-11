package connector_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/config"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/internal/testkit"
)

func TestRedisACLAndVerifiedTLS(t *testing.T) {
	t.Parallel()
	cfg, secure := testkit.NewRedisTLSContainerConfig(t)
	ctx, cancel := testkit.NewContext(t, 15*time.Second)
	defer cancel()
	t.Run("TrustedCAAndConfigSnapshot", func(t *testing.T) {
		source := *cfg
		transport := secure.Clone()
		conn, err := connector.NewRedis(&source, connector.WithRedisTLSConfig(transport))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, conn.Close()) })
		source.Username = "wrong"
		source.Password = "wrong"
		transport.ServerName = "wrong.test"
		require.NoError(t, conn.Connect(ctx))
		require.NoError(t, conn.HealthCheck(ctx))
		options := conn.GetClient().Options()
		require.Equal(t, "genesis", options.Username)
		require.Equal(t, "redis.test", options.TLSConfig.ServerName)
		require.NotSame(t, secure.RootCAs, options.TLSConfig.RootCAs)
		require.NoError(t, conn.GetClient().Set(ctx, "key", "value", time.Minute).Err())
		require.Equal(t, "value", conn.GetClient().Get(ctx, "key").Val())
	})
	t.Run("WrongCA", func(t *testing.T) {
		transport := secure.Clone()
		transport.RootCAs = x509.NewCertPool()
		conn, err := connector.NewRedis(cfg, connector.WithRedisTLSConfig(transport))
		require.NoError(t, err)
		defer conn.Close()
		err = conn.Connect(ctx)
		require.ErrorIs(t, err, connector.ErrConnection)
		var unknown x509.UnknownAuthorityError
		require.ErrorAs(t, err, &unknown)
		require.Nil(t, conn.GetClient())
	})
	t.Run("WrongHostname", func(t *testing.T) {
		transport := secure.Clone()
		transport.ServerName = "other.test"
		conn, err := connector.NewRedis(cfg, connector.WithRedisTLSConfig(transport))
		require.NoError(t, err)
		defer conn.Close()
		err = conn.Connect(ctx)
		require.ErrorIs(t, err, connector.ErrConnection)
		var hostname x509.HostnameError
		require.ErrorAs(t, err, &hostname)
	})
	t.Run("ExpiredCertificate", func(t *testing.T) {
		transport := secure.Clone()
		transport.Time = func() time.Time { return time.Now().Add(24 * time.Hour) }
		conn, err := connector.NewRedis(cfg, connector.WithRedisTLSConfig(transport))
		require.NoError(t, err)
		defer conn.Close()
		err = conn.Connect(ctx)
		require.ErrorIs(t, err, connector.ErrConnection)
		var invalid x509.CertificateInvalidError
		require.ErrorAs(t, err, &invalid)
		require.Equal(t, x509.Expired, invalid.Reason)
	})
	t.Run("WrongACLPassword", func(t *testing.T) {
		source := *cfg
		source.Password = "wrong"
		conn, err := connector.NewRedis(&source, connector.WithRedisTLSConfig(secure))
		require.NoError(t, err)
		defer conn.Close()
		err = conn.Connect(ctx)
		require.ErrorIs(t, err, connector.ErrConnection)
		require.ErrorContains(t, err, "WRONGPASS")
	})
	t.Run("WrongACLUser", func(t *testing.T) {
		source := *cfg
		source.Username = "wrong"
		conn, err := connector.NewRedis(&source, connector.WithRedisTLSConfig(secure))
		require.NoError(t, err)
		defer conn.Close()
		err = conn.Connect(ctx)
		require.ErrorIs(t, err, connector.ErrConnection)
		require.ErrorContains(t, err, "WRONGPASS")
	})
}

func TestRedisNilTLSKeepsPlaintextDefault(t *testing.T) {
	t.Parallel()
	conn, err := connector.NewRedis(testkit.NewRedisContainerConfig(t), connector.WithRedisTLSConfig(nil))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	require.NoError(t, conn.Connect(context.Background()))
	require.Nil(t, conn.GetClient().Options().TLSConfig)
}

func TestRedisACLUsernameLoadsFromEnvironment(t *testing.T) {
	t.Setenv("GENESIS_SECURITY_REDIS_USERNAME", "acl-user")
	loader, err := config.New(&config.Config{Paths: []string{t.TempDir()}, EnvPrefix: "GENESIS_SECURITY"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, loader.Close()) })
	require.NoError(t, loader.Load(context.Background()))
	var app struct {
		Redis connector.RedisConfig `mapstructure:"redis"`
	}
	require.NoError(t, loader.Unmarshal(&app))
	require.Equal(t, "acl-user", app.Redis.Username)
}

// 标准库 TLS 配置类型是有意的注入边界，不在序列化配置中放证书和私钥。
var _ connector.Option = connector.WithRedisTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12})
