package dlock

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/xerrors"
)

type controlledLease struct {
	clientv3.Lease
	grant  func(context.Context, int64) (*clientv3.LeaseGrantResponse, error)
	revoke func(context.Context, clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error)
}

func (l controlledLease) Grant(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
	return l.grant(ctx, ttl)
}

func (l controlledLease) Revoke(ctx context.Context, id clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) {
	if l.revoke != nil {
		return l.revoke(ctx, id)
	}
	return l.Lease.Revoke(ctx, id)
}

func TestEtcdDefaultSessionCreationHasIndependentWaiterBudgets(t *testing.T) {
	conn := testkit.NewEtcdContainerConnector(t)
	client := conn.GetClient()
	original := client.Lease
	entered, release := make(chan struct{}), make(chan struct{})
	var grants atomic.Int32
	client.Lease = controlledLease{Lease: original, grant: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
		grants.Add(1)
		close(entered)
		select {
		case <-release:
			return original.Grant(ctx, ttl)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	locker, err := newEtcd(conn, &Config{DefaultTTL: time.Second}, clog.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, locker.Close()) })
	require.Zero(t, grants.Load(), "New must not perform network IO")
	short, cancelShort := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { first <- locker.Lock(short, "first") }()
	<-entered
	long, cancelLong := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelLong()
	second := make(chan error, 1)
	go func() { second <- locker.Lock(long, "second") }()
	cancelShort()
	require.ErrorIs(t, <-first, context.Canceled)
	close(release)
	require.NoError(t, <-second)
	require.EqualValues(t, 1, grants.Load())
	cancelLong()
	// More than two requested lease periods: request cancellation must not
	// cancel KeepAlive, including after the initializer's timeout context ends.
	time.Sleep(2200 * time.Millisecond)
	l := locker.(*etcdLocker)
	l.mu.RLock()
	session := l.session
	l.mu.RUnlock()
	require.NotNil(t, session)
	require.NoError(t, session.Ctx().Err())
	ttl, err := original.TimeToLive(t.Context(), session.Lease())
	require.NoError(t, err)
	require.Positive(t, ttl.TTL)
	require.NoError(t, locker.Unlock(t.Context(), "second"))
}

func TestEtcdFailedSessionInitializationCanRetry(t *testing.T) {
	conn := testkit.NewEtcdContainerConnector(t)
	client := conn.GetClient()
	original := client.Lease
	failure := xerrors.New("grant unavailable")
	var calls atomic.Int32
	client.Lease = controlledLease{Lease: original, grant: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
		if calls.Add(1) == 1 {
			return nil, failure
		}
		return original.Grant(ctx, ttl)
	}}
	locker, err := newEtcd(conn, &Config{DefaultTTL: 5 * time.Second}, clog.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, locker.Close()) })
	require.ErrorIs(t, locker.Lock(t.Context(), "key"), failure)
	require.NoError(t, locker.Lock(t.Context(), "key"))
	require.EqualValues(t, 2, calls.Load())
}

func TestEtcdCloseReclaimsLateSessionLease(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom_ttl"}[custom], func(t *testing.T) {
			conn := testkit.NewEtcdContainerConnector(t)
			client := conn.GetClient()
			original := client.Lease
			granted := make(chan clientv3.LeaseID, 1)
			release := make(chan struct{})
			client.Lease = controlledLease{Lease: original, grant: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
				lease, err := original.Grant(ctx, ttl)
				if err != nil {
					return nil, err
				}
				granted <- lease.ID
				<-release // Simulate a successful RPC response arriving after Close.
				return lease, nil
			}}
			locker, err := newEtcd(conn, &Config{DefaultTTL: 5 * time.Second}, clog.Discard())
			require.NoError(t, err)
			locked := make(chan error, 1)
			go func() {
				if custom {
					locked <- locker.Lock(t.Context(), "key", WithTTL(10*time.Second))
				} else {
					locked <- locker.Lock(t.Context(), "key")
				}
			}()
			id := <-granted
			closed := make(chan error, 1)
			go func() { closed <- locker.Close() }()
			<-locker.(*etcdLocker).ctx.Done()
			close(release)
			require.NoError(t, <-closed)
			require.Error(t, <-locked)
			ttl, err := original.TimeToLive(t.Context(), id)
			require.NoError(t, err)
			require.EqualValues(t, -1, ttl.TTL, "Close must revoke late leases, not merely stop their keepalive")
			require.NoError(t, locker.Close())
		})
	}
}

func TestEtcdCustomSessionGrantHonorsRequestBudget(t *testing.T) {
	conn := testkit.NewEtcdContainerConnector(t)
	client := conn.GetClient()
	original := client.Lease
	client.Lease = controlledLease{Lease: original, grant: func(ctx context.Context, _ int64) (*clientv3.LeaseGrantResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	locker, err := newEtcd(conn, &Config{DefaultTTL: 5 * time.Second}, clog.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, locker.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- locker.Lock(ctx, "key", WithTTL(time.Second)) }()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("custom Grant ignored request deadline")
	}
	client.Lease = original
	require.NoError(t, locker.Lock(t.Context(), "key", WithTTL(time.Second)))
}

func TestEtcdTryLockReportsCustomSessionCleanupFailure(t *testing.T) {
	conn := testkit.NewEtcdContainerConnector(t)
	owner, err := newEtcd(conn, &Config{DefaultTTL: 5 * time.Second}, clog.Discard())
	require.NoError(t, err)
	contender, err := newEtcd(conn, &Config{DefaultTTL: 5 * time.Second}, clog.Discard())
	require.NoError(t, err)
	require.NoError(t, owner.Lock(t.Context(), "key"))
	client := conn.GetClient()
	original := client.Lease
	failure := xerrors.New("revoke unavailable")
	var id clientv3.LeaseID
	client.Lease = controlledLease{
		Lease: original,
		grant: func(ctx context.Context, ttl int64) (*clientv3.LeaseGrantResponse, error) {
			lease, err := original.Grant(ctx, ttl)
			if err == nil {
				id = lease.ID
			}
			return lease, err
		},
		revoke: func(context.Context, clientv3.LeaseID) (*clientv3.LeaseRevokeResponse, error) { return nil, failure },
	}
	t.Cleanup(func() {
		client.Lease = original
		if id != 0 {
			_, err := original.Revoke(context.Background(), id)
			require.NoError(t, err)
		}
		require.NoError(t, contender.Close())
		require.NoError(t, owner.Close())
	})
	acquired, err := contender.TryLock(t.Context(), "key", WithTTL(10*time.Second))
	require.False(t, acquired)
	require.ErrorIs(t, err, failure, "contention must not hide a failed lease cleanup")
}

func TestEtcdCustomSessionSurvivesAcquisitionContext(t *testing.T) {
	conn := testkit.NewEtcdContainerConnector(t)
	locker, err := newEtcd(conn, &Config{DefaultTTL: 5 * time.Second}, clog.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, locker.Close()) })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	require.NoError(t, locker.Lock(ctx, "custom", WithTTL(time.Second)))
	cancel()
	time.Sleep(2200 * time.Millisecond)
	require.NoError(t, locker.Unlock(t.Context(), "custom"))
}
