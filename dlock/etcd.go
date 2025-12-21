package dlock

import (
	"context"
	"fmt"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
)

type etcdLocker struct {
	client  *clientv3.Client
	session *concurrency.Session
	cfg     *Config
	logger  clog.Logger
	meter   metrics.Meter
	locks   map[string]*etcdLockEntry
	mu      sync.RWMutex
}

type etcdLockEntry struct {
	mutex   *concurrency.Mutex
	session *concurrency.Session
	isTTL   bool
}

// newEtcd 创建 Etcd Locker 实例
func newEtcd(conn connector.EtcdConnector, cfg *Config, logger clog.Logger, meter metrics.Meter) (Locker, error) {
	if conn == nil {
		return nil, fmt.Errorf("etcd connector is nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	client := conn.GetClient()
	// 创建默认 session，用于非 TTL 锁（或默认 TTL）
	// 注意：concurrency.Session 默认 TTL 是 60s，会自动续期
	session, err := concurrency.NewSession(client, concurrency.WithTTL(int(cfg.DefaultTTL.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd session: %w", err)
	}

	return &etcdLocker{
		client:  client,
		session: session,
		cfg:     cfg,
		logger:  logger,
		meter:   meter,
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
	l.mu.RLock()
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return fmt.Errorf("lock already held locally: %s", key)
	}
	l.mu.RUnlock()

	options := &lockOptions{
		TTL: l.cfg.DefaultTTL,
	}
	for _, opt := range opts {
		opt(options)
	}

	etcdKey := l.getEtcdKey(key)

	// 如果指定了 TTL，创建新的 session
	var session *concurrency.Session
	var err error
	if options.TTL > 0 && options.TTL != l.cfg.DefaultTTL {
		session, err = concurrency.NewSession(l.client, concurrency.WithTTL(int(options.TTL.Seconds())))
		if err != nil {
			return fmt.Errorf("failed to create etcd session: %w", err)
		}
	} else {
		session = l.session
	}

	mutex := concurrency.NewMutex(session, etcdKey)

	// 执行加锁
	var lockErr error
	if try {
		// TryLock 的情况下，使用一个很小的超时来实现非阻塞
		tryCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		lockErr = mutex.Lock(tryCtx)
		cancel()
	} else {
		lockErr = mutex.Lock(ctx)
	}

	if lockErr != nil {
		if lockErr == concurrency.ErrLocked {
			return concurrency.ErrLocked
		}
		return fmt.Errorf("failed to lock: %w", lockErr)
	}

	entry := &etcdLockEntry{
		mutex:   mutex,
		session: session,
		isTTL:   options.TTL > 0 && options.TTL != l.cfg.DefaultTTL,
	}

	l.mu.Lock()
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
		return fmt.Errorf("lock not held: %s", key)
	}
	delete(l.locks, key)
	l.mu.Unlock()

	// 释放 Mutex
	if err := entry.mutex.Unlock(ctx); err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}

	// 如果是 TTL session，需要关闭它
	if entry.isTTL && entry.session != nil {
		entry.session.Close()
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
	if l.session != nil {
		return l.session.Close()
	}
	return nil
}
