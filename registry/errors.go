package registry

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrInvalidConfig 表示 registry 配置无效。
	ErrInvalidConfig = xerrors.New("registry: invalid config")

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
)
