package idem

import (
	"context"
	"sync"
	"time"
)

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// memoryStore 内存存储实现（非导出，仅用于单机）
type memoryStore struct {
	mu      sync.Mutex
	prefix  string
	locks   map[string]time.Time
	results map[string]memoryEntry
}

func newMemoryStore(prefix string) Store {
	return &memoryStore{
		prefix:  prefix,
		locks:   make(map[string]time.Time),
		results: make(map[string]memoryEntry),
	}
}

func (ms *memoryStore) Lock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if ttl <= 0 {
		ttl = time.Second
	}

	lockKey := ms.prefix + key + lockSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	if exp, ok := ms.locks[lockKey]; ok {
		if exp.After(now) {
			return false, nil
		}
		delete(ms.locks, lockKey)
	}

	ms.locks[lockKey] = now.Add(ttl)
	return true, nil
}

func (ms *memoryStore) Unlock(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	lockKey := ms.prefix + key + lockSuffix
	ms.mu.Lock()
	delete(ms.locks, lockKey)
	ms.mu.Unlock()

	return nil
}

func (ms *memoryStore) SetResult(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = time.Second
	}

	resultKey := ms.prefix + key + resultSuffix
	lockKey := ms.prefix + key + lockSuffix
	now := time.Now()

	valCopy := append([]byte(nil), val...)

	ms.mu.Lock()
	ms.results[resultKey] = memoryEntry{
		value:     valCopy,
		expiresAt: now.Add(ttl),
	}
	delete(ms.locks, lockKey)
	ms.mu.Unlock()

	return nil
}

func (ms *memoryStore) GetResult(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	resultKey := ms.prefix + key + resultSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	entry, ok := ms.results[resultKey]
	if !ok {
		return nil, ErrResultNotFound
	}
	if entry.expiresAt.Before(now) {
		delete(ms.results, resultKey)
		return nil, ErrResultNotFound
	}

	return append([]byte(nil), entry.value...), nil
}
