package idgen

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("idgen: connector is nil")

	// ErrAlreadyAllocated WorkerID 已分配
	ErrAlreadyAllocated = xerrors.New("idgen: worker id already allocated")

	// ErrWorkerIDExhausted WorkerID 已耗尽
	ErrWorkerIDExhausted = xerrors.New("idgen: no available worker id")

	// ErrSequenceExhausted 序列号已到达配置的上限。
	ErrSequenceExhausted = xerrors.New("idgen: sequence exhausted")

	// ErrKeepAliveStarted 保活任务已经启动。
	ErrKeepAliveStarted = xerrors.New("idgen: keep alive already started")

	// ErrAllocatorStopped 分配器已经停止。
	ErrAllocatorStopped = xerrors.New("idgen: allocator stopped")

	// ErrClockBackwards 时钟回拨超过限制
	ErrClockBackwards = xerrors.New("idgen: clock moved backwards too much")

	// ErrInvalidInput 无效的输入
	ErrInvalidInput = xerrors.New("idgen: invalid input")

	// ErrLeaseExpired Etcd Lease 已过期
	ErrLeaseExpired = xerrors.New("idgen: lease expired")
)
