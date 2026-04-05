package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

type mockMySQLConnector struct {
	connector.MySQLConnector
}

type mockPostgreSQLConnector struct {
	connector.PostgreSQLConnector
}

type mockSQLiteConnector struct {
	connector.SQLiteConnector
}

type mockTracerProvider struct {
	trace.TracerProvider
}

type mockLogger struct {
	clog.Logger
}

func (m *mockLogger) WithNamespace(ns ...string) clog.Logger {
	return m
}

func TestOptions(t *testing.T) {
	t.Parallel()

	t.Run("WithMySQLConnector", func(t *testing.T) {
		mockConn := &mockMySQLConnector{}
		opt := WithMySQLConnector(mockConn)

		o := &options{}
		opt(o)

		assert.Equal(t, mockConn, o.mysqlConnector)
	})

	t.Run("WithPostgreSQLConnector", func(t *testing.T) {
		mockConn := &mockPostgreSQLConnector{}
		opt := WithPostgreSQLConnector(mockConn)

		o := &options{}
		opt(o)

		assert.Equal(t, mockConn, o.postgresqlConnector)
	})

	t.Run("WithSQLiteConnector", func(t *testing.T) {
		mockConn := &mockSQLiteConnector{}
		opt := WithSQLiteConnector(mockConn)

		o := &options{}
		opt(o)

		assert.Equal(t, mockConn, o.sqliteConnector)
	})

	t.Run("WithSilentMode", func(t *testing.T) {
		opt := WithSilentMode()

		o := &options{}
		opt(o)

		assert.True(t, o.silentMode)
	})

	t.Run("WithTracer", func(t *testing.T) {
		tracer := &mockTracerProvider{}
		opt := WithTracer(tracer)

		o := &options{}
		opt(o)

		assert.Equal(t, tracer, o.tracer)
	})

	t.Run("WithLogger", func(t *testing.T) {
		logger := &mockLogger{}
		opt := WithLogger(logger)

		o := &options{}
		opt(o)

		assert.Equal(t, logger, o.logger)
	})

	t.Run("WithLogger_Nil", func(t *testing.T) {
		opt := WithLogger(nil)

		o := &options{}
		opt(o)

		assert.Nil(t, o.logger)
	})
}
