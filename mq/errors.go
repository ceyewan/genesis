package mq

import "github.com/ceyewan/genesis/xerrors"

// 预定义错误
var (
	// ErrClosed MQ 已关闭
	ErrClosed = xerrors.New("mq: client closed")

	// ErrInvalidConfig 配置无效
	ErrInvalidConfig = xerrors.New("mq: invalid config")

	// ErrNotSupported 操作不支持
	ErrNotSupported = xerrors.New("mq: operation not supported by this driver")

	// ErrSubscriptionClosed is retained for RC1 source compatibility.
	//
	// Deprecated: subscription shutdown is reported through Done, Drain, and
	// backend/context errors; no implementation returns this sentinel.
	ErrSubscriptionClosed = xerrors.New("mq: subscription closed")

	// ErrPanicRecovered Handler panic 已恢复
	ErrPanicRecovered = xerrors.New("mq: handler panic recovered")
)
