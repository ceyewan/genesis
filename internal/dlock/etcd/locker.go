package etcd

import (
	"context"
	"fmt"
	"sync"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/dlock/types"
	telemetrytypes "github.com/ceyewan/genesis/pkg/telemetry/types"
)

type EtcdLocker struct {
	client  *clientv3.Client
	session *concurrency.Session
	cfg     *types.Config
	logger  clog.Logger
	meter   telemetrytypes.Meter
	tracer  telemetrytypes.Tracer
	locks   map[string]*etcdLockEntry
	mu      sync.RWMutex
}

type etcdLockEntry struct {
	mutex   *concurrency.Mutex
	session *concurrency.Session
	isTTL   bool
}

// New 创建 EtcdLocker 实例
func New(conn connector.EtcdConnector, cfg *types.Config, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) (*EtcdLocker, error) {
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

	return &EtcdLocker{
		client:  client,
		session: session,
		cfg:     cfg,
		logger:  logger,
		meter:   meter,
		tracer:  tracer,
		locks:   make(map[string]*etcdLockEntry),
	}, nil
}

func (l *EtcdLocker) Lock(ctx context.Context, key string, opts ...types.LockOption) error {
	return l.lock(ctx, key, false, opts...)
}

func (l *EtcdLocker) TryLock(ctx context.Context, key string, opts ...types.LockOption) (bool, error) {
	err := l.lock(ctx, key, true, opts...)
	if err != nil {
		if err == concurrency.ErrLocked {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l *EtcdLocker) lock(ctx context.Context, key string, try bool, opts ...types.LockOption) error {
	l.mu.RLock()
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return fmt.Errorf("lock already held locally: %s", key)
	}
	l.mu.RUnlock()

	options := &types.LockOptions{
		TTL: l.cfg.DefaultTTL,
	}
	for _, opt := range opts {
		opt(options)
	}

	etcdKey := l.getEtcdKey(key)
	var session *concurrency.Session
	var err error
	isTTL := false

	// 如果指定了 TTL 且与默认不同，或者需要特定的 TTL 行为，可能需要创建新的 Session
	// 这里简化处理：如果 options.TTL > 0 且不等于默认值，创建新 Session
	// 注意：etcd 的锁依赖 Session 的 Lease，Session 销毁锁也会释放
	if options.TTL > 0 && options.TTL != l.cfg.DefaultTTL {
		session, err = concurrency.NewSession(l.client, concurrency.WithTTL(int(options.TTL.Seconds())))
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
		isTTL = true
	} else {
		session = l.session
	}

	mutex := concurrency.NewMutex(session, etcdKey)

	if try {
		err = mutex.TryLock(ctx)
	} else {
		err = mutex.Lock(ctx)
	}

	if err != nil {
		if isTTL {
			session.Close()
		}
		return err
	}

	l.mu.Lock()
	l.locks[key] = &etcdLockEntry{
		mutex:   mutex,
		session: session,
		isTTL:   isTTL,
	}
	l.mu.Unlock()

	l.logger.InfoContext(ctx, "lock acquired", clog.String("key", key))
	return nil
}

func (l *EtcdLocker) Unlock(ctx context.Context, key string) error {
	l.mu.Lock()
	entry, exists := l.locks[key]
	if !exists {
		l.mu.Unlock()
		return fmt.Errorf("lock not held: %s", key)
	}
	delete(l.locks, key)
	l.mu.Unlock()

	err := entry.mutex.Unlock(ctx)
	if entry.isTTL {
		entry.session.Close()
	}

	if err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}

	l.logger.InfoContext(ctx, "lock released", clog.String("key", key))
	return nil
}

func (l *EtcdLocker) getEtcdKey(key string) string {
	if l.cfg.Prefix != "" {
		return l.cfg.Prefix + key
	}
	return key
}
