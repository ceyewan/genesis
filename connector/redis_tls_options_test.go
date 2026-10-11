package connector

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedisTLSConfigPolicySlicesAreIsolated(t *testing.T) {
	t.Parallel()
	cfg := &tls.Config{
		CipherSuites:                   []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		CurvePreferences:               []tls.CurveID{tls.CurveP256},
		NextProtos:                     []string{"initial"},
		Certificates:                   []tls.Certificate{{}},
		EncryptedClientHelloConfigList: []byte{1},
		EncryptedClientHelloKeys:       []tls.EncryptedClientHelloKey{{Config: []byte{2}}},
	}
	option := WithRedisTLSConfig(cfg)
	first, err := NewRedis(&RedisConfig{Addr: "localhost:6379"}, option)
	require.NoError(t, err)
	second, err := NewRedis(&RedisConfig{Addr: "localhost:6379"}, option)
	require.NoError(t, err)
	// 不需要连接，直接验证构造时的快照以及重复使用 Option 的隔离。
	one := first.(*redisConnector).tlsConfig
	two := second.(*redisConnector).tlsConfig
	cfg.CipherSuites[0] = tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
	cfg.CurvePreferences[0] = tls.CurveP384
	cfg.NextProtos[0] = "changed"
	cfg.Certificates[0] = tls.Certificate{OCSPStaple: []byte("changed")}
	cfg.EncryptedClientHelloConfigList[0] = 3
	cfg.EncryptedClientHelloKeys[0].Config[0] = 4
	cfg.EncryptedClientHelloKeys[0].SendAsRetry = true
	for _, snapshot := range []*tls.Config{one, two} {
		require.Equal(t, []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}, snapshot.CipherSuites)
		require.Equal(t, []tls.CurveID{tls.CurveP256}, snapshot.CurvePreferences)
		require.Equal(t, []string{"initial"}, snapshot.NextProtos)
		require.Empty(t, snapshot.Certificates[0].OCSPStaple)
		require.Equal(t, []byte{1}, snapshot.EncryptedClientHelloConfigList)
		require.Equal(t, []byte{2}, snapshot.EncryptedClientHelloKeys[0].Config)
		require.False(t, snapshot.EncryptedClientHelloKeys[0].SendAsRetry)
	}
	one.CipherSuites[0] = tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
	one.CurvePreferences[0] = tls.CurveP384
	one.NextProtos[0] = "connector-one"
	require.Equal(t, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, two.CipherSuites[0])
	require.Equal(t, tls.CurveP256, two.CurvePreferences[0])
	require.Equal(t, "initial", two.NextProtos[0])
}

func TestRedisTLSConfigPreservesNilPolicySlices(t *testing.T) {
	t.Parallel()
	conn, err := NewRedis(&RedisConfig{Addr: "localhost:6379"}, WithRedisTLSConfig(&tls.Config{}))
	require.NoError(t, err)
	cfg := conn.(*redisConnector).tlsConfig
	require.Nil(t, cfg.CipherSuites)
	require.Nil(t, cfg.CurvePreferences)
	require.Nil(t, cfg.NextProtos)
	require.Nil(t, cfg.Certificates)
	require.Nil(t, cfg.EncryptedClientHelloConfigList)
	require.Nil(t, cfg.EncryptedClientHelloKeys)
}
