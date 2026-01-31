package db

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// Option 配置 DB 实例的选项
type Option func(*options)

// options 内部选项结构
type options struct {
	logger              clog.Logger
	tracer              trace.TracerProvider
	mysqlConnector      connector.MySQLConnector
	postgresqlConnector connector.PostgreSQLConnector
	sqliteConnector     connector.SQLiteConnector
	silentMode          bool // 静默模式，禁用 SQL 日志输出
}

// WithLogger 注入日志记录器
func WithLogger(l clog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l.WithNamespace("db")
		}
	}
}

// WithTracer 注入 TracerProvider（用于 OpenTelemetry trace）
func WithTracer(tp trace.TracerProvider) Option {
	return func(o *options) {
		o.tracer = tp
	}
}

// WithMySQLConnector 注入 MySQL 连接器
func WithMySQLConnector(conn connector.MySQLConnector) Option {
	return func(o *options) {
		o.mysqlConnector = conn
	}
}

// WithPostgreSQLConnector 注入 PostgreSQL 连接器
func WithPostgreSQLConnector(conn connector.PostgreSQLConnector) Option {
	return func(o *options) {
		o.postgresqlConnector = conn
	}
}

// WithSQLiteConnector 注入 SQLite 连接器
func WithSQLiteConnector(conn connector.SQLiteConnector) Option {
	return func(o *options) {
		o.sqliteConnector = conn
	}
}

// WithSilentMode 启用静默模式，禁用 SQL 日志输出
// 适用于测试环境或不需要 SQL 日志的场景
func WithSilentMode() Option {
	return func(o *options) {
		o.silentMode = true
	}
}
