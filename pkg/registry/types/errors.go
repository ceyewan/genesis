package types

import "errors"

var (
	// ErrServiceNotFound 服务未找到
	ErrServiceNotFound = errors.New("service not found")

	// ErrServiceAlreadyRegistered 服务已注册
	ErrServiceAlreadyRegistered = errors.New("service already registered")

	// ErrInvalidServiceInstance 无效的服务实例
	ErrInvalidServiceInstance = errors.New("invalid service instance")

	// ErrLeaseExpired 租约已过期
	ErrLeaseExpired = errors.New("lease expired")

	// ErrWatchClosed Watch 已关闭
	ErrWatchClosed = errors.New("watch closed")

	// ErrConnectionFailed 连接失败
	ErrConnectionFailed = errors.New("connection failed")
)
