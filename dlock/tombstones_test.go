package dlock

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoundedTombstonesEvictOldestEntry(t *testing.T) {
	tombstones := newBoundedTombstones[int]()
	for i := range lostTombstoneLimit + 1 {
		tombstones.Put(fmt.Sprintf("key-%d", i), i)
	}

	require.Equal(t, lostTombstoneLimit, tombstones.Len())
	_, retained := tombstones.Get("key-0")
	require.False(t, retained)
	value, retained := tombstones.Get(fmt.Sprintf("key-%d", lostTombstoneLimit))
	require.True(t, retained)
	require.Equal(t, lostTombstoneLimit, value)
}

func TestRedisLostTombstonesStayBoundedUnderConcurrentOwnershipLoss(t *testing.T) {
	const extraEntries = 128
	const entries = lostTombstoneLimit + extraEntries

	locker := &redisLocker{
		locks: make(map[string]*redisLockEntry, entries),
		lost:  newBoundedTombstones[*redisLockEntry](),
	}
	type keyedEntry struct {
		key   string
		entry *redisLockEntry
	}
	all := make([]keyedEntry, 0, entries)
	for i := range entries {
		key := fmt.Sprintf("key-%d", i)
		entry := &redisLockEntry{lostCh: make(chan error, 1)}
		locker.locks[key] = entry
		all = append(all, keyedEntry{key: key, entry: entry})
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, item := range all {
		wg.Go(func() {
			<-start
			locker.markOwnershipLost(item.key, item.entry)
		})
	}
	close(start)
	wg.Wait()

	locker.mu.RLock()
	require.Empty(t, locker.locks)
	require.Equal(t, lostTombstoneLimit, locker.lost.Len())
	locker.mu.RUnlock()

	// Tombstone eviction only bounds delayed key lookup. Every channel obtained
	// with the live entry still receives its ownership-loss notification.
	for _, item := range all {
		require.ErrorIs(t, <-item.entry.lostCh, ErrOwnershipLost)
		_, open := <-item.entry.lostCh
		require.False(t, open)
	}
}
