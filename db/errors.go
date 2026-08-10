package db

import (
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

var (
	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = xerrors.New("db: invalid config")

	// ErrMySQLConnectorRequired MySQL 连接器未提供
	ErrMySQLConnectorRequired = xerrors.New("db: mysql connector is required")

	// ErrPostgreSQLConnectorRequired PostgreSQL 连接器未提供
	ErrPostgreSQLConnectorRequired = xerrors.New("db: postgresql connector is required")

	// ErrSQLiteConnectorRequired SQLite 连接器未提供
	ErrSQLiteConnectorRequired = xerrors.New("db: sqlite connector is required")

	// ErrConnectorNotReady 表示连接器尚未 Connect 或已被关闭。
	// New 返回此错误时也会保留 connector.ErrClientNil 分类。
	ErrConnectorNotReady = xerrors.New("db: connector is not ready")

	// ErrNilTransaction 表示 Transaction 收到了 nil 回调。
	ErrNilTransaction = xerrors.New("db: transaction function is nil")
)

func newConnectorNotReadyError(driver string) error {
	return xerrors.Wrapf(
		xerrors.Join(ErrConnectorNotReady, connector.ErrClientNil),
		"%s connector returned a nil client",
		driver,
	)
}
