package idem

import "github.com/ceyewan/genesis/xerrors"

// 错误定义
var (
	// ErrConfigNil 配置为空
	ErrConfigNil = xerrors.New("idem: config is nil")

	// ErrKeyEmpty 幂等键为空
	ErrKeyEmpty = xerrors.New("idem: key is empty")

	// ErrConcurrentRequest 并发请求
	ErrConcurrentRequest = xerrors.New("idem: concurrent request detected")

	// ErrResultNotFound 结果未找到（内部使用）
	ErrResultNotFound = xerrors.New("idem: result not found")
)
