package coord

import "context"

// KV provides basic key/value operations with watch support.
type KV interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, value []byte, opts ...Option) error
	Delete(ctx context.Context, key string, opts ...Option) error
	Watch(ctx context.Context, key string, opts ...Option) (Watcher, error)
}

// Locker exposes distributed locking primitives.
type Locker interface {
	Lock(ctx context.Context, key string) (Lock, error)
}

// Lock represents a distributed lock guard.
type Lock interface {
	Unlock(ctx context.Context) error
	Refresh(ctx context.Context) error
}

// LeaseManager manages leases with automatic renewal.
type LeaseManager interface {
	Grant(ctx context.Context) (Lease, error)
}

// Lease encapsulates a renewable lease handle.
type Lease interface {
	ID() int64
	KeepAlive(ctx context.Context) (<-chan struct{}, error)
	Revoke(ctx context.Context) error
}

// Provider aggregates coordination capabilities.
type Provider interface {
	KV
	Locker
	LeaseManager
}

// Watcher streams key change events.
type Watcher interface {
	Events() <-chan Event
	Close() error
}

// Event describes a change for a watched key or prefix.
type Event struct {
	Type  EventType
	Key   string
	Value []byte
}

// EventType distinguishes watch events.
type EventType int

const (
	// EventPut indicates the key has been created or updated.
	EventPut EventType = iota
	// EventDelete indicates the key has been deleted.
	EventDelete
)

// Option customises coordinator operations.
type Option interface{}
