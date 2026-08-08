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
	mu        sync.Mutex
	prefix    string
	locks     map[string]lockEntry
	results   map[string]memoryEntry
	stopCh    chan struct{}
	closeOnce sync.Once
	workerWG  sync.WaitGroup
}

func newMemoryStore(prefix string) Store {
	return newMemoryStoreWithCleanup(prefix, time.Minute)
}

func newMemoryStoreWithCleanup(prefix string, interval time.Duration) Store {
	if interval <= 0 {
		interval = time.Minute
	}
	ms := &memoryStore{
		prefix:  prefix,
		locks:   make(map[string]lockEntry),
		results: make(map[string]memoryEntry),
		stopCh:  make(chan struct{}),
	}
	ms.workerWG.Go(func() {
		ms.cleanup(interval)
	})
	return ms
}

func (ms *memoryStore) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case now := <-ticker.C:
			ms.removeExpired(now)
		case <-ms.stopCh:
			return
		}
	}
}

func (ms *memoryStore) removeExpired(now time.Time) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	for key, entry := range ms.locks {
		if !entry.expiresAt.After(now) {
			delete(ms.locks, key)
		}
	}
	for key, entry := range ms.results {
		if !entry.expiresAt.After(now) {
			delete(ms.results, key)
		}
	}
}

func (ms *memoryStore) Close() error {
	ms.closeOnce.Do(func() {
		close(ms.stopCh)
	})
	ms.workerWG.Wait()
	return nil
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
	if token != "" {
		entry, ok := ms.locks[lockKey]
		if !ok || entry.token != token || !entry.expiresAt.After(now) {
			ms.mu.Unlock()
			return ErrLockLost
		}
	}
	ms.results[resultKey] = memoryEntry{
		value:     valCopy,
		expiresAt: now.Add(ttl),
	}
	if token != "" {
		delete(ms.locks, lockKey)
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
		return ErrLockLost
	}
	entry.expiresAt = now.Add(ttl)
	ms.locks[lockKey] = entry
	return nil
}

func (ms *memoryStore) DeleteResult(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	resultKey := ms.prefix + key + resultSuffix

	ms.mu.Lock()
	delete(ms.results, resultKey)
	ms.mu.Unlock()

	return nil
}
