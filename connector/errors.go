package connector

import "github.com/ceyewan/genesis/xerrors"

// Sentinel Errors - 连接器专用的哨兵错误
var (
	ErrNotConnected  = xerrors.New("connector: not connected")
	ErrAlreadyClosed = xerrors.New("connector: already closed")
	ErrConnection    = xerrors.New("connector: connection failed")
	ErrTimeout       = xerrors.New("connector: timeout")
	ErrConfig        = xerrors.New("connector: invalid config")
	ErrHealthCheck   = xerrors.New("connector: health check failed")
)
