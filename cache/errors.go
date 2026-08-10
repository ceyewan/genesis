package cache

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrInvalidConfig 表示缓存配置无效。
	ErrInvalidConfig = xerrors.New("cache: invalid config")

	// ErrMiss 表示缓存未命中。
	ErrMiss = xerrors.New("cache: miss")

	// ErrNotSupported is retained for RC1 source compatibility. Cache
	// implementations do not return it.
	//
	// Deprecated: no cache operation uses this sentinel.
	ErrNotSupported = xerrors.New("cache: operation not supported")

	// ErrInvalidDestination 表示读取操作的目标值类型无效。
	ErrInvalidDestination = xerrors.New("cache: invalid destination")

	// ErrRedisConnectorRequired 表示分布式缓存缺少 Redis 连接器。
	ErrRedisConnectorRequired = xerrors.New("cache: redis connector is required")

	// ErrLocalCacheRequired 表示多级缓存缺少本地缓存实例。
	ErrLocalCacheRequired = xerrors.New("cache: local cache is required")

	// ErrRemoteCacheRequired 表示多级缓存缺少远程缓存实例。
	ErrRemoteCacheRequired = xerrors.New("cache: remote cache is required")

	// ErrInvalidTTL 表示缓存 TTL 为负值。
	ErrInvalidTTL = xerrors.New("cache: ttl must not be negative")
)
