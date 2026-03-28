package breaker

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrInvalidConfig 配置无效。
	ErrInvalidConfig = xerrors.New("breaker: invalid config")

	// ErrKeyEmpty 熔断键为空。
	ErrKeyEmpty = xerrors.New("breaker: key is empty")

	// ErrOpenState 熔断器处于打开状态。
	ErrOpenState = xerrors.New("breaker: circuit breaker is open")

	// ErrTooManyRequests 表示半开状态下的探测请求数已达到上限。
	ErrTooManyRequests = xerrors.New("breaker: too many requests while circuit breaker is half-open")
)
