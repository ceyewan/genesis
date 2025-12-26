package ratelimit

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("ratelimit: config is nil")

	// ErrConnectorNil 连接器为空
	ErrConnectorNil = xerrors.New("ratelimit: connector is nil")

	// ErrNotSupported 操作不支持
	ErrNotSupported = xerrors.New("ratelimit: operation not supported")

	// ErrKeyEmpty 限流键为空
	ErrKeyEmpty = xerrors.New("ratelimit: key is empty")

	// ErrInvalidLimit 限流规则无效
	ErrInvalidLimit = xerrors.New("ratelimit: invalid limit")

	// ErrRateLimitExceeded 限流阈值超出
	ErrRateLimitExceeded = xerrors.New("ratelimit: rate limit exceeded")
)
