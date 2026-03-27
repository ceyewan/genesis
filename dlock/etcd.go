package dlock

import (
	"context"
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
	mu      sync.RWMutex

	closeOnce sync.Once
	closeErr  error
}

type etcdLockEntry struct {
	mutex   *concurrency.Mutex
	session *concurrency.Session
	isTTL   bool
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
	}, nil
}

func (l *etcdLocker) Lock(ctx context.Context, key string, opts ...LockOption) error {
	return l.lock(ctx, key, false, opts...)
}

func (l *etcdLocker) TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error) {
	err := l.lock(ctx, key, true, opts...)
	if err != nil {
		if err == concurrency.ErrLocked {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l *etcdLocker) lock(ctx context.Context, key string, try bool, opts ...LockOption) error {
	// 检查本地是否已持有锁（防止同一 locker 重复获取同一把锁）
	l.mu.RLock()
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
		if lockErr == concurrency.ErrLocked {
			return concurrency.ErrLocked
		}
		return xerrors.Wrap(lockErr, "failed to lock")
	}

	entry := &etcdLockEntry{
		mutex:   mutex,
		session: session,
		isTTL:   ttl != l.cfg.DefaultTTL,
	}

	l.mu.Lock()
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		_ = mutex.Unlock(ctx)
		if entry.isTTL && entry.session != nil {
			_ = entry.session.Close()
		}
		return xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}
	l.locks[key] = entry
	l.mu.Unlock()

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock acquired", clog.String("key", key))
	}
	return nil
}

func (l *etcdLocker) Unlock(ctx context.Context, key string) error {
	l.mu.Lock()
	entry, exists := l.locks[key]
	if !exists {
		l.mu.Unlock()
		return xerrors.Wrapf(ErrLockNotHeld, "key: %s", key)
	}
	delete(l.locks, key)
	l.mu.Unlock()

	// 释放 Mutex
	if err := entry.mutex.Unlock(ctx); err != nil {
		return xerrors.Wrap(err, "failed to unlock")
	}

	// 如果是 TTL session，需要关闭它
	if entry.isTTL && entry.session != nil {
		_ = entry.session.Close()
	}

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock released", clog.String("key", key))
	}
	return nil
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
		entries := make(map[string]*etcdLockEntry, len(l.locks))
		for key, entry := range l.locks {
			entries[key] = entry
		}
		l.locks = make(map[string]*etcdLockEntry)
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
