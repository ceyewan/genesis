package idem

import (
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// classifiedSentinel preserves a specific sentinel and message while making
// errors.Is also match a broader public error class.
type classifiedSentinel struct {
	message string
	class   error
}

func (e *classifiedSentinel) Error() string { return e.message }

func (e *classifiedSentinel) Unwrap() error { return e.class }

// 错误定义
var (
	// ErrInvalidConfig 表示 idem 配置无效。
	ErrInvalidConfig = xerrors.New("idem: invalid config")

	// ErrConfigNil 配置为空。它同时匹配 ErrInvalidConfig。
	ErrConfigNil error = &classifiedSentinel{
		message: "idem: config is nil",
		class:   ErrInvalidConfig,
	}

	// ErrConnectorNil 表示 Redis connector 缺失或尚未 Connect。
	// 它同时匹配 connector.ErrClientNil。
	ErrConnectorNil error = &classifiedSentinel{
		message: "idem: connector is nil",
		class:   connector.ErrClientNil,
	}

	// ErrKeyEmpty 幂等键为空
	ErrKeyEmpty = xerrors.New("idem: key is empty")

	// ErrKeyTooLong 表示客户端提供的原始幂等 key 超过配置的字节上限。
	ErrKeyTooLong = xerrors.New("idem: key exceeds the configured byte limit")

	// ErrConcurrentRequest 并发请求
	ErrConcurrentRequest = xerrors.New("idem: concurrent request detected")

	// ErrLockLost 表示执行过程中丢失了幂等锁
	ErrLockLost = xerrors.New("idem: lock lost during execution")

	// ErrKeyConflict 表示同一 endpoint 下的幂等 key 被用于不同请求。
	ErrKeyConflict = xerrors.New("idem: key reused with a different request")

	// ErrStoreCapacity 表示内置 Memory 后端没有容量接纳新的逻辑 key。
	ErrStoreCapacity = xerrors.New("idem: store capacity exceeded")

	// ErrResultNotFound 结果未找到（内部使用）
	ErrResultNotFound = xerrors.New("idem: result not found")
)
