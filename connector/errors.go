package connector

import "github.com/ceyewan/genesis/xerrors"

// Sentinel Errors - 连接器专用的哨兵错误
var (
	// ErrConnection 连接建立失败
	ErrConnection = xerrors.New("connector: connection failed")

	// ErrConfig 配置无效
	ErrConfig = xerrors.New("connector: invalid config")

	// ErrHealthCheck 健康检查失败
	ErrHealthCheck = xerrors.New("connector: health check failed")

	// ErrClientNil 客户端为空（未初始化或已关闭）
	ErrClientNil = xerrors.New("connector: client is nil")
)
