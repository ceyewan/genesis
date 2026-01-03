package idgen

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("idgen: connector is nil")

	// ErrWorkerIDExhausted WorkerID 已耗尽
	ErrWorkerIDExhausted = xerrors.New("idgen: no available worker id")

	// ErrClockBackwards 时钟回拨超过限制
	ErrClockBackwards = xerrors.New("idgen: clock moved backwards too much")

	// ErrInvalidInput 无效的输入
	ErrInvalidInput = xerrors.New("idgen: invalid input")

	// ErrLeaseExpired Etcd Lease 已过期
	ErrLeaseExpired = xerrors.New("idgen: lease expired")
)
