package dlock

import (
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

// classifiedSentinel preserves an existing sentinel's identity and message
// while also making it match a broader error class through errors.Is.
type classifiedSentinel struct {
	message string
	class   error
}

func (e *classifiedSentinel) Error() string { return e.message }

func (e *classifiedSentinel) Unwrap() error { return e.class }

var (
	// ErrInvalidConfig 表示 dlock 配置无效。
	ErrInvalidConfig = xerrors.New("dlock: invalid config")

	// ErrConfigNil 配置为空。它同时匹配 ErrInvalidConfig。
	ErrConfigNil error = &classifiedSentinel{
		message: "dlock: config is nil",
		class:   ErrInvalidConfig,
	}

	// ErrConnectorNil 表示所需连接器缺失或尚未就绪。
	// 它同时匹配 connector.ErrClientNil。
	ErrConnectorNil error = &classifiedSentinel{
		message: "dlock: connector is nil",
		class:   connector.ErrClientNil,
	}

	// ErrInvalidKey 表示锁 key 无效。
	ErrInvalidKey = xerrors.New("dlock: invalid key")

	// ErrLockNotHeld 锁未持有
	ErrLockNotHeld = xerrors.New("dlock: lock not held")

	// ErrLockAlreadyHeld 锁已在本地持有
	ErrLockAlreadyHeld = xerrors.New("dlock: lock already held locally")

	// ErrOwnershipLost 锁所有权丢失
	ErrOwnershipLost = xerrors.New("dlock: ownership lost")

	// ErrInvalidTTL TTL 配置非法。它同时匹配 ErrInvalidConfig。
	ErrInvalidTTL error = &classifiedSentinel{
		message: "dlock: invalid ttl",
		class:   ErrInvalidConfig,
	}

	// ErrClosed Locker 已关闭。
	ErrClosed = xerrors.New("dlock: locker closed")
)
