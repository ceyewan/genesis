package ratelimit

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("ratelimit: config is nil")

	// ErrInvalidConfig 表示非 nil 配置无效。
	ErrInvalidConfig = xerrors.New("ratelimit: invalid config")

	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("ratelimit: connector is nil")

	// ErrNotSupported 操作不支持
	ErrNotSupported = xerrors.New("ratelimit: operation not supported")

	// ErrKeyEmpty 限流键为空
	ErrKeyEmpty = xerrors.New("ratelimit: key is empty")

	// ErrInvalidLimit 限流规则无效
	ErrInvalidLimit = xerrors.New("ratelimit: invalid limit")

	// ErrRateLimitExceeded is retained for RC1 source compatibility.
	//
	// Deprecated: a denied request is represented by allowed=false; no limiter
	// operation returns this sentinel.
	ErrRateLimitExceeded = xerrors.New("ratelimit: rate limit exceeded")

	// ErrKeyLimitExceeded 表示单机限流器已达到 StandaloneConfig.MaxKeys。
	ErrKeyLimitExceeded = xerrors.New("ratelimit: key limit exceeded")

	// ErrLimiterClosed 表示限流器已经关闭，Close 是终态。
	ErrLimiterClosed = xerrors.New("ratelimit: limiter is closed")
)
