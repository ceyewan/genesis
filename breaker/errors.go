package breaker

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("breaker: config is nil")

	// ErrKeyEmpty 熔断键为空
	ErrKeyEmpty = xerrors.New("breaker: key is empty")

	// ErrOpenState 熔断器处于打开状态
	ErrOpenState = xerrors.New("breaker: circuit breaker is open")
)
