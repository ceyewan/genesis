package dlock

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/xerrors"
)

// A response may arrive after cancellation. Keep the first Delete in flight
// until Close has begun, then let the real etcd operation complete.
type pausedReleaseKV struct {
	clientv3.KV
	entered    chan struct{}
	release    chan struct{}
	calls      atomic.Int32
	firstError error
}

func (kv *pausedReleaseKV) Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	if kv.calls.Add(1) == 1 {
		close(kv.entered)
		<-kv.release
		if kv.firstError != nil {
			return nil, kv.firstError
		}
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	return kv.KV.Delete(cleanup, key, opts...)
}

func TestEtcdCloseWaitsForInFlightUnlockLeaseCleanup(t *testing.T) {
	conn := testkit.NewEtcdContainerConnector(t)
	locker, err := newEtcd(conn, &Config{DefaultTTL: 10 * time.Second}, clog.Discard())
	require.NoError(t, err)
	require.NoError(t, locker.Lock(t.Context(), "cleanup", WithTTL(20*time.Second)))
	client := conn.GetClient()
	original := client.Lease
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client.Lease = controlledLease{Lease: original, revoke: func(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return original.Revoke(ctx, id)
	}}
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); _ = locker.Close(); client.Lease = original })
	unlocked, closed := make(chan error, 1), make(chan error, 1)
	go func() { unlocked <- locker.Unlock(t.Context(), "cleanup") }()
	<-entered
	go func() { closed <- locker.Close() }()
	<-locker.(*etcdLocker).ctx.Done()
	var premature bool
	var closeErr error
	select {
	case closeErr = <-closed:
		premature = true
	case <-time.After(20 * time.Millisecond):
	}
	unblock()
	require.NoError(t, <-unlocked)
	if !premature {
		closeErr = <-closed
	}
	require.NoError(t, closeErr)
	require.False(t, premature, "Close missed cleanup after the entry left the lock map")
	require.EqualValues(t, 1, calls.Load())
}

func TestEtcdCloseSerializesConcurrentUnlock(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, failFirst := range []bool{false, true} {
			name := "default"
			if custom {
				name = "custom"
			}
			if failFirst {
				name += "/retry"
			} else {
				name += "/success"
			}
			t.Run(name, func(t *testing.T) {
				conn := testkit.NewEtcdContainerConnector(t)
				locker, err := newEtcd(conn, &Config{DefaultTTL: 10 * time.Second}, clog.Discard())
				require.NoError(t, err)
				var opts []LockOption
				if custom {
					opts = append(opts, WithTTL(20*time.Second))
				}
				require.NoError(t, locker.Lock(t.Context(), "release", opts...))
				client := conn.GetClient()
				original := client.KV
				kv := &pausedReleaseKV{KV: original, entered: make(chan struct{}), release: make(chan struct{})}
				if failFirst {
					kv.firstError = xerrors.New("release unavailable")
				}
				client.KV = kv
				var once sync.Once
				release := func() { once.Do(func() { close(kv.release) }) }
				t.Cleanup(func() { release(); _ = locker.Close(); client.KV = original })
				unlocked, closed := make(chan error, 1), make(chan error, 1)
				go func() { unlocked <- locker.Unlock(t.Context(), "release") }()
				<-kv.entered
				go func() { closed <- locker.Close() }()
				<-locker.(*etcdLocker).ctx.Done()
				release()
				unlockErr := <-unlocked
				closeErr := <-closed
				if failFirst {
					require.ErrorIs(t, unlockErr, kv.firstError)
					require.EqualValues(t, 2, kv.calls.Load(), "Close must retry a failed release")
				} else {
					require.NoError(t, unlockErr)
					require.EqualValues(t, 1, kv.calls.Load(), "a successful release must not run twice")
				}
				require.NoError(t, closeErr)
				require.NoError(t, locker.Close())
			})
		}
	}
}
