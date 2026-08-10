package idem

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ceyewan/genesis/xerrors"
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
	mu         sync.Mutex
	prefix     string
	locks      map[string]lockEntry
	results    map[string]memoryEntry
	entries    map[string]struct{}
	maxEntries int
	stopCh     chan struct{}
	closeOnce  sync.Once
	workerWG   sync.WaitGroup
}

func newMemoryStore(prefix string) Store {
	return newMemoryStoreWithLimit(prefix, defaultMemoryMaxEntries)
}

func newMemoryStoreWithLimit(prefix string, maxEntries int) Store {
	return newMemoryStoreWithCleanupAndLimit(prefix, time.Minute, maxEntries)
}

func newMemoryStoreWithCleanup(prefix string, interval time.Duration) Store {
	return newMemoryStoreWithCleanupAndLimit(prefix, interval, defaultMemoryMaxEntries)
}

func newMemoryStoreWithCleanupAndLimit(prefix string, interval time.Duration, maxEntries int) Store {
	if interval <= 0 {
		interval = time.Minute
	}
	if maxEntries <= 0 {
		maxEntries = defaultMemoryMaxEntries
	}
	ms := &memoryStore{
		prefix:     prefix,
		locks:      make(map[string]lockEntry),
		results:    make(map[string]memoryEntry),
		entries:    make(map[string]struct{}),
		maxEntries: maxEntries,
		stopCh:     make(chan struct{}),
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
	ms.removeExpiredLocked(now)
}

func (ms *memoryStore) removeExpiredLocked(now time.Time) {
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

	clear(ms.entries)
	for key := range ms.locks {
		ms.entries[strings.TrimSuffix(key, lockSuffix)] = struct{}{}
	}
	for key := range ms.results {
		ms.entries[strings.TrimSuffix(key, resultSuffix)] = struct{}{}
	}
}

func (ms *memoryStore) removeExpiredEntryLocked(entryKey string, now time.Time) {
	lockKey := entryKey + lockSuffix
	if entry, ok := ms.locks[lockKey]; ok && !entry.expiresAt.After(now) {
		delete(ms.locks, lockKey)
	}
	resultKey := entryKey + resultSuffix
	if entry, ok := ms.results[resultKey]; ok && !entry.expiresAt.After(now) {
		delete(ms.results, resultKey)
	}
	ms.reconcileEntryLocked(entryKey)
}

func (ms *memoryStore) reconcileEntryLocked(entryKey string) {
	_, hasLock := ms.locks[entryKey+lockSuffix]
	_, hasResult := ms.results[entryKey+resultSuffix]
	if !hasLock && !hasResult {
		delete(ms.entries, entryKey)
	}
}

func (ms *memoryStore) reserveEntryLocked(entryKey string, now time.Time) error {
	if _, ok := ms.entries[entryKey]; ok {
		return nil
	}
	if len(ms.entries) >= ms.maxEntries {
		// Opportunistically reclaim every expired entry before rejecting a new
		// key. This runs under the same mutex as admission, so cleanup and the
		// capacity decision have one linearization point.
		ms.removeExpiredLocked(now)
	}
	if len(ms.entries) >= ms.maxEntries {
		return xerrors.Wrapf(
			ErrStoreCapacity,
			"idem: memory store contains %d logical entries; maximum is %d",
			len(ms.entries),
			ms.maxEntries,
		)
	}
	ms.entries[entryKey] = struct{}{}
	return nil
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

	entryKey := ms.prefix + key
	lockKey := entryKey + lockSuffix
	resultKey := entryKey + resultSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.removeExpiredEntryLocked(entryKey, now)
	if _, ok := ms.results[resultKey]; ok {
		return "", false, nil
	}

	if _, ok := ms.locks[lockKey]; ok {
		return "", false, nil
	}
	if err := ms.reserveEntryLocked(entryKey, now); err != nil {
		return "", false, err
	}

	token, err := newLockToken()
	if err != nil {
		ms.reconcileEntryLocked(entryKey)
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

	entryKey := ms.prefix + key
	lockKey := entryKey + lockSuffix
	ms.mu.Lock()
	if entry, ok := ms.locks[lockKey]; ok && entry.token == token {
		delete(ms.locks, lockKey)
	}
	ms.reconcileEntryLocked(entryKey)
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

	entryKey := ms.prefix + key
	resultKey := entryKey + resultSuffix
	lockKey := entryKey + lockSuffix
	now := time.Now()

	valCopy := append([]byte(nil), val...)

	ms.mu.Lock()
	ms.removeExpiredEntryLocked(entryKey, now)
	if token != "" {
		entry, ok := ms.locks[lockKey]
		if !ok || entry.token != token {
			ms.mu.Unlock()
			return ErrLockLost
		}
	} else if err := ms.reserveEntryLocked(entryKey, now); err != nil {
		ms.mu.Unlock()
		return err
	}
	ms.results[resultKey] = memoryEntry{
		value:     valCopy,
		expiresAt: now.Add(ttl),
	}
	if token != "" {
		delete(ms.locks, lockKey)
	}
	ms.entries[entryKey] = struct{}{}
	ms.mu.Unlock()

	return nil
}

func (ms *memoryStore) GetResult(ctx context.Context, key string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entryKey := ms.prefix + key
	resultKey := entryKey + resultSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.removeExpiredEntryLocked(entryKey, now)
	entry, ok := ms.results[resultKey]
	if !ok {
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

	entryKey := ms.prefix + key
	lockKey := entryKey + lockSuffix
	now := time.Now()

	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.removeExpiredEntryLocked(entryKey, now)
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

	entryKey := ms.prefix + key
	resultKey := entryKey + resultSuffix

	ms.mu.Lock()
	delete(ms.results, resultKey)
	ms.reconcileEntryLocked(entryKey)
	ms.mu.Unlock()

	return nil
}
