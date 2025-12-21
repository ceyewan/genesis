package breaker

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("breaker: config is nil")

	// ErrServiceNameEmpty 服务名为空
	ErrServiceNameEmpty = xerrors.New("breaker: service name is empty")

	// ErrBreakerNotFound 熔断器未找到
	ErrBreakerNotFound = xerrors.New("breaker: circuit breaker not found")

	// ErrOpenState 熔断器处于打开状态
	ErrOpenState = xerrors.New("breaker: circuit breaker is open")
)

