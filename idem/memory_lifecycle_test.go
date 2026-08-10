package idem

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMemoryStoreCleansExpiredEntries(t *testing.T) {
	t.Parallel()

	store := newMemoryStoreWithCleanup("cleanup:", 5*time.Millisecond).(*memoryStore)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	token, locked, err := store.Lock(context.Background(), "lock", 10*time.Millisecond)
	require.NoError(t, err)
	require.True(t, locked)
	require.NotEmpty(t, token)
	require.NoError(t, store.SetResult(context.Background(), "result", []byte("value"), 10*time.Millisecond, ""))

	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return len(store.locks) == 0 && len(store.results) == 0
	}, time.Second, 5*time.Millisecond)
}

func TestMemoryStoreConcurrentClose(t *testing.T) {
	t.Parallel()

	store := newMemoryStoreWithCleanup("close:", time.Hour).(*memoryStore)
	const callers = 64
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			errs <- store.Close()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestIdempotencyCloseDoesNotOwnRedisStore(t *testing.T) {
	t.Parallel()

	component := newIdempotency(&Config{}, &nonClosingStore{})
	require.NoError(t, component.Close())
}

func TestIdempotencyClassifiesWrappedResultMiss(t *testing.T) {
	t.Parallel()

	component := newIdempotency(&Config{
		DefaultTTL: time.Minute,
		LockTTL:    time.Minute,
	}, &wrappedMissStore{})

	result, err := component.Execute(t.Context(), "execute", func(context.Context) (any, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	require.Equal(t, "ok", result)

	processed, err := component.Consume(t.Context(), "consume", time.Minute, func(context.Context) error {
		return nil
	})
	require.NoError(t, err)
	require.True(t, processed)
}

type nonClosingStore struct{}

type wrappedMissStore struct{}

func (*wrappedMissStore) Lock(context.Context, string, time.Duration) (LockToken, bool, error) {
	return "token", true, nil
}

func (*wrappedMissStore) Unlock(context.Context, string, LockToken) error { return nil }

func (*wrappedMissStore) SetResult(context.Context, string, []byte, time.Duration, LockToken) error {
	return nil
}

func (*wrappedMissStore) GetResult(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("wrapped miss: %w", ErrResultNotFound)
}

func (*nonClosingStore) Lock(context.Context, string, time.Duration) (LockToken, bool, error) {
	return "", false, nil
}

func (*nonClosingStore) Unlock(context.Context, string, LockToken) error { return nil }

func (*nonClosingStore) SetResult(context.Context, string, []byte, time.Duration, LockToken) error {
	return nil
}

func (*nonClosingStore) GetResult(context.Context, string) ([]byte, error) {
	return nil, ErrResultNotFound
}
