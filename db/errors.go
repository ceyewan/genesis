package db

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = xerrors.New("db: invalid config")

	// ErrMySQLConnectorRequired MySQL 连接器未提供
	ErrMySQLConnectorRequired = xerrors.New("db: mysql connector is required")

	// ErrPostgreSQLConnectorRequired PostgreSQL 连接器未提供
	ErrPostgreSQLConnectorRequired = xerrors.New("db: postgresql connector is required")

	// ErrSQLiteConnectorRequired SQLite 连接器未提供
	ErrSQLiteConnectorRequired = xerrors.New("db: sqlite connector is required")
)
