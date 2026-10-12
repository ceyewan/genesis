package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pausedWriteRemote struct {
	Distributed
	entered chan struct{}
	release chan struct{}
}

func (r *pausedWriteRemote) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	close(r.entered)
	select {
	case <-r.release:
		return r.Distributed.Set(ctx, key, value, ttl)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMultiCoordinationHonorsCancellation(t *testing.T) {
	t.Parallel()
	local, err := NewLocal(&LocalConfig{MaxEntries: 10})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, local.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, local.Set(ctx, "hot", "old", time.Minute))
	remote := &pausedWriteRemote{
		Distributed: newMockKVForMulti(),
		entered:     make(chan struct{}),
		release:     make(chan struct{}),
	}
	multi, err := NewMulti(local, remote, nil)
	require.NoError(t, err)
	writeDone := make(chan error, 1)
	go func() { writeDone <- multi.Set(ctx, "slow", "value", time.Minute) }()
	// 即使断言失败也释放远端 I/O，并等待写操作结束后再关闭 Local。
	t.Cleanup(func() {
		close(remote.release)
		require.NoError(t, <-writeDone)
	})
	select {
	case <-remote.entered:
	case <-ctx.Done():
		t.Fatal("Write did not enter remote I/O")
	}

	operations := []struct {
		name string
		call func(context.Context) error
	}{
		{"Get", func(ctx context.Context) error { var value string; return multi.Get(ctx, "hot", &value) }},
		{"Has", func(ctx context.Context) error { _, err := multi.Has(ctx, "hot"); return err }},
		{"Set", func(ctx context.Context) error { return multi.Set(ctx, "hot", "new", time.Minute) }},
		{"Delete", func(ctx context.Context) error { return multi.Delete(ctx, "hot") }},
		{"Expire", func(ctx context.Context) error { _, err := multi.Expire(ctx, "hot", time.Second); return err }},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			request, requestCancel := context.WithTimeout(ctx, 20*time.Millisecond)
			defer requestCancel()
			done := make(chan error, 1)
			go func() { done <- operation.call(request) }()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case <-time.After(time.Second):
				t.Fatal("Canceled operation is still waiting for an unrelated write")
			}
		})
	}
	var value string
	require.NoError(t, local.Get(ctx, "hot", &value))
	require.Equal(t, "old", value, "Canceled mutations must not reach storage")
}

func TestMultiAlreadyCanceledContext(t *testing.T) {
	t.Parallel()
	local, err := NewLocal(&LocalConfig{MaxEntries: 10})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, local.Close()) })
	require.NoError(t, local.Set(t.Context(), "hot", "old", time.Minute))
	multi, err := NewMulti(local, newMockKVForMulti(), nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var value string
	require.ErrorIs(t, multi.Get(ctx, "hot", &value), context.Canceled)
	_, err = multi.Has(ctx, "hot")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, multi.Set(ctx, "hot", "new", time.Minute), context.Canceled)
	require.ErrorIs(t, multi.Delete(ctx, "hot"), context.Canceled)
	_, err = multi.Expire(ctx, "hot", time.Second)
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, local.Get(t.Context(), "hot", &value))
	require.Equal(t, "old", value)
}
