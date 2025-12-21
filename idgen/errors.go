package idgen

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("idgen: config is nil")

	// ErrInvalidMethod 无效的方法
	ErrInvalidMethod = xerrors.New("idgen: invalid method")

	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("idgen: connector is nil")

	// ErrWorkerIDExhausted WorkerID 已耗尽
	ErrWorkerIDExhausted = xerrors.New("idgen: no available worker id")

	// ErrClockBackwards 时钟回拨超过限制
	ErrClockBackwards = xerrors.New("idgen: clock moved backwards too much")

	// ErrWorkerIDLost WorkerID 租约失效
	ErrWorkerIDLost = xerrors.New("idgen: worker id lease lost")

	// ErrUnsupportedVersion 不支持的 UUID 版本
	ErrUnsupportedVersion = xerrors.New("idgen: unsupported version")

	// ErrInvalidInput 无效的输入
	ErrInvalidInput = xerrors.New("idgen: invalid input")
)
