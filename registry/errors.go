package registry

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrServiceNotFound 服务未找到
	ErrServiceNotFound = xerrors.New("service not found")

	// ErrServiceAlreadyRegistered 服务已注册
	ErrServiceAlreadyRegistered = xerrors.New("service already registered")

	// ErrInvalidServiceInstance 无效的服务实例
	ErrInvalidServiceInstance = xerrors.New("invalid service instance")

	// ErrRegistryAlreadyInitialized registry 已初始化
	ErrRegistryAlreadyInitialized = xerrors.New("registry already initialized")

	// ErrRegistryClosed registry 已关闭
	ErrRegistryClosed = xerrors.New("registry is closed")

	// ErrInvalidTTL 无效的 TTL
	ErrInvalidTTL = xerrors.New("invalid ttl")

	// ErrLeaseExpired 租约已过期
	ErrLeaseExpired = xerrors.New("lease expired")

	// ErrWatchClosed Watch 已关闭
	ErrWatchClosed = xerrors.New("watch closed")

	// ErrConnectionFailed 连接失败
	ErrConnectionFailed = xerrors.New("connection failed")
)
