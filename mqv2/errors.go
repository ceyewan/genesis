package mqv2

import "github.com/ceyewan/genesis/xerrors"

// 预定义错误
var (
	// ErrClosed MQ 已关闭
	ErrClosed = xerrors.New("mq: client closed")

	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = xerrors.New("mq: invalid config")

	// ErrNotSupported 操作不支持
	ErrNotSupported = xerrors.New("mq: operation not supported by this driver")

	// ErrSubscriptionClosed 订阅已关闭
	ErrSubscriptionClosed = xerrors.New("mq: subscription closed")

	// ErrPanicRecovered Handler panic 已恢复
	ErrPanicRecovered = xerrors.New("mq: handler panic recovered")
)
