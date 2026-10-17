package connector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

//nolint:staticcheck // Intentionally exercise the nil-context rejection contract.
func TestConnectRejectsNilContextBeforeIO(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		connect func() error
	}{
		{"Redis", func() error { return new(redisConnector).Connect(nil) }},
		{"Etcd", func() error { return new(etcdConnector).Connect(nil) }},
		{"NATS", func() error { return new(natsConnector).Connect(nil) }},
		{"Kafka", func() error { return new(kafkaConnector).Connect(nil) }},
		{"MySQL", func() error { return new(mysqlConnector).Connect(nil) }},
		{"PostgreSQL", func() error { return new(postgresqlConnector).Connect(nil) }},
		{"SQLite", func() error { return new(sqliteConnector).Connect(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) { require.ErrorIs(t, tc.connect(), ErrConfig) })
	}
}

func TestNilConnectorOptionIsIgnored(t *testing.T) {
	t.Parallel()
	conn, err := NewSQLite(&SQLiteConfig{Path: ":memory:"}, nil)
	require.NoError(t, err)
	require.NoError(t, conn.Connect(t.Context()))
	require.NoError(t, conn.Close())
}
