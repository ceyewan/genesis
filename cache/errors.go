package cache

import "github.com/ceyewan/genesis/xerrors"

var (
	// ErrMiss 表示缓存未命中。
	ErrMiss = xerrors.New("cache: miss")

	// ErrNotSupported 表示当前缓存实现不支持该操作。
	ErrNotSupported = xerrors.New("cache: operation not supported")

	// ErrRedisConnectorRequired 表示分布式缓存缺少 Redis 连接器。
	ErrRedisConnectorRequired = xerrors.New("cache: redis connector is required")

	// ErrLocalCacheRequired 表示多级缓存缺少本地缓存实例。
	ErrLocalCacheRequired = xerrors.New("cache: local cache is required")

	// ErrRemoteCacheRequired 表示多级缓存缺少远程缓存实例。
	ErrRemoteCacheRequired = xerrors.New("cache: remote cache is required")
)
