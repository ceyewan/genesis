package dlock

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("dlock: config is nil")

	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("dlock: connector is nil")

	// ErrLockNotHeld 锁未持有
	ErrLockNotHeld = xerrors.New("dlock: lock not held")

	// ErrLockAlreadyHeld 锁已在本地持有
	ErrLockAlreadyHeld = xerrors.New("dlock: lock already held locally")

	// ErrOwnershipLost 锁所有权丢失
	ErrOwnershipLost = xerrors.New("dlock: ownership lost")
)
