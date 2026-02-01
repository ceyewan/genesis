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

type lockEntry struct {
	token     LockToken
	expiresAt time.Time
}

// memoryStore 内存存储实现（非导出，仅用于单机）
type memoryStore struct {
	mu      sync.Mutex
	prefix  string
	locks   map[string]lockEntry
	results map[string]memoryEntry
}

func newMemoryStore(prefix string) Store {
	return &memoryStore{
		prefix:  prefix,
		locks:   make(map[string]lockEntry),
		results: make(map[string]memoryEntry),
	}
}

func (ms *memoryStore) Lock(ctx context.Context, key string, ttl time.Duration) (LockToken, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if ttl <= 0 {
		ttl = time.Second
	}

	lockKey := ms.prefix + key + lockSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	if exp, ok := ms.locks[lockKey]; ok {
		if exp.expiresAt.After(now) {
			return "", false, nil
		}
		delete(ms.locks, lockKey)
	}

	token, err := newLockToken()
	if err != nil {
		return "", false, err
	}

	ms.locks[lockKey] = lockEntry{token: token, expiresAt: now.Add(ttl)}
	return token, true, nil
}

func (ms *memoryStore) Unlock(ctx context.Context, key string, token LockToken) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if token == "" {
		return nil
	}

	lockKey := ms.prefix + key + lockSuffix
	ms.mu.Lock()
	if entry, ok := ms.locks[lockKey]; ok && entry.token == token {
		delete(ms.locks, lockKey)
	}
	ms.mu.Unlock()

	return nil
}

func (ms *memoryStore) SetResult(ctx context.Context, key string, val []byte, ttl time.Duration, token LockToken) error {
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
	if token != "" {
		if entry, ok := ms.locks[lockKey]; ok && entry.token == token {
			delete(ms.locks, lockKey)
		}
	}
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

func (ms *memoryStore) Refresh(ctx context.Context, key string, token LockToken, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if token == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Second
	}

	lockKey := ms.prefix + key + lockSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	entry, ok := ms.locks[lockKey]
	if !ok || entry.token != token {
		return nil
	}
	entry.expiresAt = now.Add(ttl)
	ms.locks[lockKey] = entry
	return nil
}
