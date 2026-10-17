package dlock

import (
	"context"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

const (
	etcdSessionCreateTimeout = 5 * time.Second
	etcdLeaseCleanupTimeout  = 2 * time.Second
)

type sessionAttempt struct {
	done    chan struct{}
	session *concurrency.Session
	err     error
}

// defaultSession shares creation work, not the first caller's deadline.
// Failed attempts are removed so later requests can retry.
func (l *etcdLocker) defaultSession(ctx context.Context) (*concurrency.Session, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	if l.session != nil {
		select {
		case <-l.session.Done():
			l.session = nil
		default:
			session := l.session
			l.mu.Unlock()
			return session, nil
		}
	}
	attempt := l.initializing
	if attempt == nil {
		attempt = &sessionAttempt{done: make(chan struct{})}
		l.initializing = attempt
		l.workers.Add(1)
		go l.initializeSession(attempt)
	}
	l.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-attempt.done:
		return attempt.session, attempt.err
	}
}

func (l *etcdLocker) initializeSession(attempt *sessionAttempt) {
	defer l.workers.Done()
	ctx, cancel := context.WithTimeout(l.ctx, etcdSessionCreateTimeout)
	defer cancel()
	session, err := l.createSession(ctx, l.cfg.DefaultTTL)
	l.mu.Lock()
	closed := l.closed
	if err == nil && !closed {
		l.session = session
	}
	l.mu.Unlock()
	if closed && session != nil {
		l.recordCleanupError(l.closeSession(session))
		session = nil
		err = ErrClosed
	}
	l.mu.Lock()
	attempt.session, attempt.err = session, err
	l.initializing = nil
	close(attempt.done)
	l.mu.Unlock()
}

// Grant has an operation budget; KeepAlive has the Locker's lifetime.
func (l *etcdLocker) createSession(ctx context.Context, ttl time.Duration) (*concurrency.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, etcdSessionCreateTimeout)
	defer cancel()
	lease, err := l.client.Grant(ctx, int64(ttl/time.Second))
	if err != nil {
		return nil, xerrors.Wrap(err, "grant etcd session lease")
	}
	session, err := concurrency.NewSession(l.client,
		concurrency.WithLease(lease.ID),
		concurrency.WithTTL(int(ttl/time.Second)),
		concurrency.WithContext(l.ctx))
	if err != nil {
		cleanupErr := l.revokeLease(lease.ID)
		l.recordCleanupError(cleanupErr)
		return nil, xerrors.Combine(xerrors.Wrap(err, "start etcd session keepalive"), cleanupErr)
	}
	if err := ctx.Err(); err != nil {
		cleanupErr := l.closeSession(session)
		l.recordCleanupError(cleanupErr)
		return nil, xerrors.Combine(err, cleanupErr)
	}
	return session, nil
}

func (l *etcdLocker) revokeLease(id clientv3.LeaseID) error {
	ctx, cancel := context.WithTimeout(context.Background(), etcdLeaseCleanupTimeout)
	defer cancel()
	_, err := l.client.Revoke(ctx, id)
	return xerrors.Wrap(err, "revoke etcd session lease")
}

func (l *etcdLocker) closeSession(session *concurrency.Session) error {
	session.Orphan()
	return l.revokeLease(session.Lease())
}

func (l *etcdLocker) recordCleanupError(err error) {
	if err == nil {
		return
	}
	l.mu.Lock()
	closed := l.closed
	if closed {
		l.cleanupErrors = append(l.cleanupErrors, err)
	}
	l.mu.Unlock()
	if !closed && l.logger != nil {
		l.logger.Error("Etcd lease cleanup failed", clog.Error(err))
	}
}

// cleanupAcquisition only receives unpublished entries: no Unlock or Close
// snapshot can access their Mutex. The acquiring worker owns all cleanup.
func (l *etcdLocker) cleanupAcquisition(entry *etcdLockEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), etcdLeaseCleanupTimeout)
	defer cancel()
	l.recordCleanupError(entry.mutex.Unlock(ctx))
	if entry.isTTL {
		l.recordCleanupError(l.closeSession(entry.session))
	}
}
