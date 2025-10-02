package cache

import (
	"context"
	"time"
)

// Cache defines the minimal read/write contract exposed to callers.
type Cache interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value any, expiration time.Duration) error
	Delete(ctx context.Context, keys ...string) error
}

// Locker represents distributed lock behaviour backed by the cache store.
type Locker interface {
	Acquire(ctx context.Context, key string, expiration time.Duration) (Lock, error)
}

// Lock is held by a caller after Acquire succeeds. The lock must be released
// explicitly to avoid blocking other workers.
type Lock interface {
	Release(ctx context.Context) error
	Refresh(ctx context.Context, expiration time.Duration) error
}
