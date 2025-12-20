package registry

import "github.com/ceyewan/genesis/pkg/xerrors"

var (
	// ErrServiceNotFound 服务未找到
	ErrServiceNotFound = xerrors.New("service not found")

	// ErrServiceAlreadyRegistered 服务已注册
	ErrServiceAlreadyRegistered = xerrors.New("service already registered")

	// ErrInvalidServiceInstance 无效的服务实例
	ErrInvalidServiceInstance = xerrors.New("invalid service instance")

	// ErrLeaseExpired 租约已过期
	ErrLeaseExpired = xerrors.New("lease expired")

	// ErrWatchClosed Watch 已关闭
	ErrWatchClosed = xerrors.New("watch closed")

	// ErrConnectionFailed 连接失败
	ErrConnectionFailed = xerrors.New("connection failed")
)