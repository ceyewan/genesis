package dlock

import (
	"context"
	"maps"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

type etcdLocker struct {
	client  *clientv3.Client
	session *concurrency.Session
	cfg     *Config
	logger  clog.Logger
	locks   map[string]*etcdLockEntry
	lost    map[string]*etcdLockEntry
	mu      sync.RWMutex

	closeOnce sync.Once
	closeErr  error
	closed    bool
}

type etcdLockEntry struct {
	mutex       *concurrency.Mutex
	session     *concurrency.Session
	isTTL       bool
	lostCh      chan error
	lostOnce    sync.Once
	monitorStop chan struct{}
	monitorDone chan struct{}
	stopOnce    sync.Once
	releaseMu   sync.Mutex
}

// newEtcd 创建 Etcd Locker 实例
func newEtcd(conn connector.EtcdConnector, cfg *Config, logger clog.Logger) (Locker, error) {
	if conn == nil {
		return nil, ErrConnectorNil
	}
	if cfg == nil {
		return nil, ErrConfigNil
	}

	client := conn.GetClient()
	if client == nil {
		return nil, xerrors.Wrap(ErrConnectorNil, "etcd connector is not connected")
	}
	// 创建默认 session，用于非 TTL 锁（或默认 TTL）
	// 注意：concurrency.Session 默认 TTL 是 60s，会自动续期
	session, err := concurrency.NewSession(client, concurrency.WithTTL(int(cfg.DefaultTTL.Seconds())))
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to create etcd session")
	}

	return &etcdLocker{
		client:  client,
		session: session,
		cfg:     cfg,
		logger:  logger,
		locks:   make(map[string]*etcdLockEntry),
		lost:    make(map[string]*etcdLockEntry),
	}, nil
}

func (l *etcdLocker) Lock(ctx context.Context, key string, opts ...LockOption) error {
	return l.lock(ctx, key, false, opts...)
}

func (l *etcdLocker) TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error) {
	err := l.lock(ctx, key, true, opts...)
	if err != nil {
		if xerrors.Is(err, concurrency.ErrLocked) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l *etcdLocker) lock(ctx context.Context, key string, try bool, opts ...LockOption) error {
	// 检查本地是否已持有锁（防止同一 locker 重复获取同一把锁）
	l.mu.RLock()
	if l.closed {
		l.mu.RUnlock()
		return ErrClosed
	}
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}
	l.mu.RUnlock()

	ttl, err := resolveLockTTL(l.cfg.DefaultTTL, opts...)
	if err != nil {
		return err
	}
	if err := validateEtcdTTL(ttl); err != nil {
		return err
	}

	etcdKey := l.getEtcdKey(key)

	// 如果指定了 TTL，创建新的 session
	var session *concurrency.Session
	if ttl != l.cfg.DefaultTTL {
		session, err = concurrency.NewSession(l.client, concurrency.WithTTL(int(ttl.Seconds())))
		if err != nil {
			return xerrors.Wrap(err, "failed to create etcd session")
		}
	} else {
		session = l.session
	}

	mutex := concurrency.NewMutex(session, etcdKey)

	// 执行加锁
	var lockErr error
	if try {
		// 使用官方 TryLock API 而不是超时 hack
		lockErr = mutex.TryLock(ctx)
	} else {
		lockErr = mutex.Lock(ctx)
	}

	if lockErr != nil {
		// 如果是新创建的 session 且加锁失败，需要关闭
		if ttl != l.cfg.DefaultTTL && session != nil {
			_ = session.Close()
		}
		if xerrors.Is(lockErr, concurrency.ErrLocked) {
			return concurrency.ErrLocked
		}
		return xerrors.Wrap(lockErr, "failed to lock")
	}

	entry := &etcdLockEntry{
		mutex:       mutex,
		session:     session,
		isTTL:       ttl != l.cfg.DefaultTTL,
		lostCh:      make(chan error, 1),
		monitorStop: make(chan struct{}),
		monitorDone: make(chan struct{}),
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = mutex.Unlock(ctx)
		if entry.isTTL && entry.session != nil {
			_ = entry.session.Close()
		}
		return ErrClosed
	}
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		_ = mutex.Unlock(ctx)
		if entry.isTTL && entry.session != nil {
			_ = entry.session.Close()
		}
		return xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}
	l.locks[key] = entry
	delete(l.lost, key)
	l.mu.Unlock()
	go l.monitorOwnership(key, entry)

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock acquired", clog.String("key", key))
	}
	return nil
}

func (l *etcdLocker) Unlock(ctx context.Context, key string) error {
	l.mu.RLock()
	entry, exists := l.locks[key]
	if !exists {
		_, lost := l.lost[key]
		l.mu.RUnlock()
		if lost {
			return xerrors.Wrapf(ErrOwnershipLost, "key: %s", key)
		}
		return xerrors.Wrapf(ErrLockNotHeld, "key: %s", key)
	}
	l.mu.RUnlock()

	entry.releaseMu.Lock()
	defer entry.releaseMu.Unlock()
	l.mu.RLock()
	current := l.locks[key]
	l.mu.RUnlock()
	if current != entry {
		return xerrors.Wrapf(ErrLockNotHeld, "key: %s", key)
	}

	// 释放 Mutex
	if err := entry.mutex.Unlock(ctx); err != nil {
		return xerrors.Wrap(err, "failed to unlock")
	}
	l.removeReleased(key, entry)

	// 如果是 TTL session，需要关闭它
	if entry.isTTL && entry.session != nil {
		_ = entry.session.Close()
	}

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock released", clog.String("key", key))
	}
	return nil
}

func (l *etcdLocker) Lost(key string) <-chan error {
	l.mu.RLock()
	entry := l.locks[key]
	if entry == nil {
		entry = l.lost[key]
	}
	l.mu.RUnlock()
	if entry != nil {
		return entry.lostCh
	}
	ch := make(chan error, 1)
	ch <- xerrors.Wrapf(ErrLockNotHeld, "key: %s", key)
	close(ch)
	return ch
}

func (l *etcdLocker) monitorOwnership(key string, entry *etcdLockEntry) {
	defer close(entry.monitorDone)
	select {
	case <-entry.session.Done():
		l.markOwnershipLost(key, entry)
	case <-entry.monitorStop:
	}
}

func (l *etcdLocker) markOwnershipLost(key string, entry *etcdLockEntry) {
	l.mu.Lock()
	if l.locks[key] == entry {
		delete(l.locks, key)
		l.lost[key] = entry
		entry.lostOnce.Do(func() {
			entry.lostCh <- xerrors.Wrapf(ErrOwnershipLost, "key: %s", key)
			close(entry.lostCh)
		})
	}
	l.mu.Unlock()
}

func (l *etcdLocker) stopMonitor(entry *etcdLockEntry) {
	entry.stopOnce.Do(func() { close(entry.monitorStop) })
	<-entry.monitorDone
}

func (l *etcdLocker) removeReleased(key string, entry *etcdLockEntry) {
	l.mu.Lock()
	if l.locks[key] == entry {
		delete(l.locks, key)
	}
	l.mu.Unlock()
	l.stopMonitor(entry)
	entry.lostOnce.Do(func() { close(entry.lostCh) })
}

func (l *etcdLocker) getEtcdKey(key string) string {
	if l.cfg.Prefix != "" {
		return l.cfg.Prefix + key
	}
	return key
}

// Close 关闭 Etcd Locker，释放 session
func (l *etcdLocker) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		entries := make(map[string]*etcdLockEntry, len(l.locks))
		maps.Copy(entries, l.locks)
		l.locks = make(map[string]*etcdLockEntry)
		l.lost = make(map[string]*etcdLockEntry)
		defaultSession := l.session
		l.session = nil
		l.mu.Unlock()

		var errs []error
		for key, entry := range entries {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if err := entry.mutex.Unlock(ctx); err != nil {
				errs = append(errs, xerrors.Wrapf(err, "failed to unlock key: %s during close", key))
			}
			cancel()
			l.stopMonitor(entry)
			entry.lostOnce.Do(func() { close(entry.lostCh) })

			if entry.isTTL && entry.session != nil {
				if err := entry.session.Close(); err != nil {
					errs = append(errs, xerrors.Wrapf(err, "failed to close ttl session for key: %s", key))
				}
			}
		}

		if defaultSession != nil {
			if err := defaultSession.Close(); err != nil {
				errs = append(errs, xerrors.Wrap(err, "failed to close default etcd session"))
			}
		}

		l.closeErr = xerrors.Combine(errs...)
	})
	return l.closeErr
}
