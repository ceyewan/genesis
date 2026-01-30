package connector

import "github.com/ceyewan/genesis/xerrors"

// Sentinel Errors - 连接器专用的哨兵错误
var (
	// ErrNotConnected 连接器尚未建立连接
	ErrNotConnected = xerrors.New("connector: not connected")

	// ErrAlreadyClosed 连接器已关闭
	ErrAlreadyClosed = xerrors.New("connector: already closed")

	// ErrAlreadyConnected 连接器已连接（用于幂等性检查）
	ErrAlreadyConnected = xerrors.New("connector: already connected")

	// ErrConnection 连接建立失败
	ErrConnection = xerrors.New("connector: connection failed")

	// ErrTimeout 操作超时
	ErrTimeout = xerrors.New("connector: timeout")

	// ErrConfig 配置无效
	ErrConfig = xerrors.New("connector: invalid config")

	// ErrHealthCheck 健康检查失败
	ErrHealthCheck = xerrors.New("connector: health check failed")

	// ErrClientNil 客户端为空（未初始化或已关闭）
	ErrClientNil = xerrors.New("connector: client is nil")
)
