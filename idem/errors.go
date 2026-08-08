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

	// ErrLockLost 表示执行过程中丢失了幂等锁
	ErrLockLost = xerrors.New("idem: lock lost during execution")

	// ErrKeyConflict 表示同一 endpoint 下的幂等 key 被用于不同请求。
	ErrKeyConflict = xerrors.New("idem: key reused with a different request")

	// ErrResultNotFound 结果未找到（内部使用）
	ErrResultNotFound = xerrors.New("idem: result not found")
)
