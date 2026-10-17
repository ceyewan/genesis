package dlock

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
)

type pausedAcquireHook struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (h *pausedAcquireHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *pausedAcquireHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *pausedAcquireHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" && h.calls.Add(1) == 1 {
			close(h.entered)
			select {
			case <-h.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return next(ctx, cmd)
	}
}

func TestRedisReservesLocalKeyBeforeRemoteAcquire(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	locker, err := newRedis(conn, &Config{DefaultTTL: time.Minute}, clog.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, locker.Close()) })
	hook := &pausedAcquireHook{entered: make(chan struct{}), release: make(chan struct{})}
	conn.GetClient().AddHook(hook)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := locker.TryLock(ctx, "key"); first <- err }()
	<-hook.entered
	_, secondErr := locker.TryLock(ctx, "key")
	close(hook.release)
	require.NoError(t, <-first)
	require.ErrorIs(t, secondErr, ErrLockAlreadyHeld)
	require.EqualValues(t, 1, hook.calls.Load())
}

func TestFailedRedisAcquirePreservesOwnershipLost(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	locker, err := newRedis(conn, &Config{DefaultTTL: time.Minute}, clog.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, locker.Close()) })
	l := locker.(*redisLocker)
	entry := &redisLockEntry{lostCh: make(chan error, 1)}
	l.mu.Lock()
	l.lost.Put("key", entry)
	l.mu.Unlock()
	require.NoError(t, conn.GetClient().Set(t.Context(), "key", "other-owner", time.Minute).Err())
	locked, err := locker.TryLock(t.Context(), "key")
	require.NoError(t, err)
	require.False(t, locked)
	require.Equal(t, (<-chan error)(entry.lostCh), locker.Lost("key"))
	require.ErrorIs(t, locker.Unlock(t.Context(), "key"), ErrOwnershipLost)
}
