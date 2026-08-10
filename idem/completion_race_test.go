package idem

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/internal/testkit"
)

func TestMemoryCompletionBetweenResultMissAndLockDoesNotReexecute(t *testing.T) {
	store := newMemoryStore("test:idem:completion-race:memory:")
	t.Cleanup(func() { require.NoError(t, store.(closableStore).Close()) })
	testCompletionBetweenResultMissAndLock(t, store)
}

func TestRedisCompletionBetweenResultMissAndLockDoesNotReexecute(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	store := newRedisStore(conn, "test:idem:completion-race:redis:"+testkit.NewID()+":")
	testCompletionBetweenResultMissAndLock(t, store)
}

func TestNonAtomicCustomStoreIsProtectedByPostLockResultCheck(t *testing.T) {
	store := newMemoryStore("test:idem:completion-race:custom:")
	t.Cleanup(func() { require.NoError(t, store.(closableStore).Close()) })
	const key = "execute"
	ownerToken, locked, err := store.Lock(t.Context(), key, time.Minute)
	require.NoError(t, err)
	require.True(t, locked)

	resultBlind := &resultBlindLockStore{Store: store}
	gated := newCompletionRaceStore(resultBlind)
	component := newIdempotency(&Config{
		DefaultTTL:   time.Minute,
		LockTTL:      time.Minute,
		WaitInterval: time.Millisecond,
	}, gated)

	type outcome struct {
		result any
		err    error
	}
	done := make(chan outcome, 1)
	var executions atomic.Int32
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() {
		result, executeErr := component.Execute(ctx, key, func(context.Context) (any, error) {
			executions.Add(1)
			return "duplicate", nil
		})
		done <- outcome{result: result, err: executeErr}
	}()

	<-gated.initialMissRead
	require.NoError(t, store.SetResult(ctx, key, []byte(`"owner"`), time.Minute, ownerToken))
	close(gated.returnInitialMiss)

	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, "owner", got.result)
	require.Equal(t, int32(0), executions.Load())
	require.True(t, resultBlind.redundantUnlock.Load())
}

func testCompletionBetweenResultMissAndLock(t *testing.T, store Store) {
	t.Helper()

	t.Run("Execute", func(t *testing.T) {
		const key = "execute"
		ownerToken, locked, err := store.Lock(t.Context(), key, time.Minute)
		require.NoError(t, err)
		require.True(t, locked)

		gated := newCompletionRaceStore(store)
		component := newIdempotency(&Config{
			DefaultTTL:   time.Minute,
			LockTTL:      time.Minute,
			WaitInterval: time.Millisecond,
		}, gated)

		type outcome struct {
			result any
			err    error
		}
		done := make(chan outcome, 1)
		var executions atomic.Int32
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		go func() {
			result, executeErr := component.Execute(ctx, key, func(context.Context) (any, error) {
				executions.Add(1)
				return map[string]any{"source": "duplicate"}, nil
			})
			done <- outcome{result: result, err: executeErr}
		}()

		<-gated.initialMissRead
		require.NoError(t, store.SetResult(ctx, key, []byte(`{"source":"owner"}`), time.Minute, ownerToken))
		close(gated.returnInitialMiss)

		got := <-done
		require.NoError(t, got.err)
		require.Equal(t, int32(0), executions.Load())
		require.Equal(t, map[string]any{"source": "owner"}, got.result)
		require.False(t, gated.lockAcquired.Load(), "built-in stores must atomically refuse a lock when a result exists")
		assertNoRedundantLock(t, store, key)
	})

	t.Run("Consume", func(t *testing.T) {
		const key = "consume"
		ownerToken, locked, err := store.Lock(t.Context(), key, time.Minute)
		require.NoError(t, err)
		require.True(t, locked)

		gated := newCompletionRaceStore(store)
		component := newIdempotency(&Config{
			DefaultTTL: time.Minute,
			LockTTL:    time.Minute,
		}, gated)

		type outcome struct {
			executed bool
			err      error
		}
		done := make(chan outcome, 1)
		var executions atomic.Int32
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		go func() {
			executed, consumeErr := component.Consume(ctx, key, time.Minute, func(context.Context) error {
				executions.Add(1)
				return nil
			})
			done <- outcome{executed: executed, err: consumeErr}
		}()

		<-gated.initialMissRead
		require.NoError(t, store.SetResult(ctx, key, []byte(processedMarker), time.Minute, ownerToken))
		close(gated.returnInitialMiss)

		got := <-done
		require.NoError(t, got.err)
		require.False(t, got.executed)
		require.Equal(t, int32(0), executions.Load())
		require.False(t, gated.lockAcquired.Load(), "built-in stores must atomically refuse a lock when a result exists")
		assertNoRedundantLock(t, store, key)
	})
}

func assertNoRedundantLock(t *testing.T, store Store, key string) {
	t.Helper()
	deletable, ok := store.(DeletableStore)
	require.True(t, ok)
	require.NoError(t, deletable.DeleteResult(t.Context(), key))
	token, locked, err := store.Lock(t.Context(), key, time.Minute)
	require.NoError(t, err)
	require.True(t, locked, "no processing lock may remain after observing the completed result")
	require.NoError(t, store.Unlock(t.Context(), key, token))
}

// completionRaceStore freezes the first completed-result miss after the read
// has happened. The test can then commit the previous owner's result before
// allowing the caller to proceed to Lock, reproducing the exact TOCTOU window.
type completionRaceStore struct {
	Store
	getCalls          atomic.Int32
	lockAcquired      atomic.Bool
	initialMissRead   chan struct{}
	returnInitialMiss chan struct{}
}

func (s *completionRaceStore) Lock(ctx context.Context, key string, ttl time.Duration) (LockToken, bool, error) {
	token, locked, err := s.Store.Lock(ctx, key, ttl)
	s.lockAcquired.Store(locked)
	return token, locked, err
}

// resultBlindLockStore models a pre-existing third-party Store whose Lock does
// not inspect the completed-result key. The shared post-lock check must still
// protect users of that implementation without adding a method to Store.
type resultBlindLockStore struct {
	Store
	redundantUnlock atomic.Bool
}

func (s *resultBlindLockStore) Lock(context.Context, string, time.Duration) (LockToken, bool, error) {
	return "synthetic-redundant-lock", true, nil
}

func (s *resultBlindLockStore) Unlock(ctx context.Context, key string, token LockToken) error {
	if token == "synthetic-redundant-lock" {
		s.redundantUnlock.Store(true)
		return nil
	}
	return s.Store.Unlock(ctx, key, token)
}

func newCompletionRaceStore(store Store) *completionRaceStore {
	return &completionRaceStore{
		Store:             store,
		initialMissRead:   make(chan struct{}),
		returnInitialMiss: make(chan struct{}),
	}
}

func (s *completionRaceStore) GetResult(ctx context.Context, key string) ([]byte, error) {
	result, err := s.Store.GetResult(ctx, key)
	if s.getCalls.Add(1) != 1 {
		return result, err
	}
	close(s.initialMissRead)
	select {
	case <-s.returnInitialMiss:
		return result, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
