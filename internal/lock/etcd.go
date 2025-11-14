package lock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	"github.com/ceyewan/genesis/pkg/lock"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// EtcdLocker 基于 etcd 的分布式锁实现
type EtcdLocker struct {
	client  *clientv3.Client
	session *concurrency.Session
	options *lock.LockOptions
	mu      sync.RWMutex
	locks   map[string]*concurrency.Mutex // 维护已持有的锁
}

// NewEtcdLocker 创建 etcd 分布式锁实例（兼容现有API）
func NewEtcdLocker(opts *lock.LockOptions) (*EtcdLocker, error) {
	if opts == nil {
		opts = lock.DefaultLockOptions()
	}

	// 使用默认配置创建连接
	connConfig := connector.ConnectionConfig{
		Backend:   "etcd",
		Endpoints: []string{"127.0.0.1:2379"},
		Timeout:   5 * time.Second,
	}

	// 从连接管理器获取 etcd 客户端
	manager := connector.GetManager()
	client, err := manager.GetEtcdClient(connConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd client: %w", err)
	}

	// 创建 session，用于租约管理
	session, err := concurrency.NewSession(client, concurrency.WithTTL(int(opts.TTL.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &EtcdLocker{
		client:  client,
		session: session,
		options: opts,
		locks:   make(map[string]*concurrency.Mutex),
	}, nil
}

// Lock 阻塞式加锁
func (l *EtcdLocker) Lock(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查是否已经持有该锁
	if _, exists := l.locks[key]; exists {
		return fmt.Errorf("lock already held: %s", key)
	}

	// 创建互斥锁
	mutex := concurrency.NewMutex(l.session, key)

	// 阻塞式获取锁
	if err := mutex.Lock(ctx); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// 记录已持有的锁
	l.locks[key] = mutex

	return nil
}

// TryLock 非阻塞式加锁
func (l *EtcdLocker) TryLock(ctx context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查是否已经持有该锁
	if _, exists := l.locks[key]; exists {
		return false, fmt.Errorf("lock already held: %s", key)
	}

	// 创建互斥锁
	mutex := concurrency.NewMutex(l.session, key)

	// 尝试获取锁
	if err := mutex.TryLock(ctx); err != nil {
		if err == concurrency.ErrLocked {
			return false, nil
		}
		return false, fmt.Errorf("failed to try lock: %w", err)
	}

	// 记录已持有的锁
	l.locks[key] = mutex

	return true, nil
}

// Unlock 释放锁
func (l *EtcdLocker) Unlock(ctx context.Context, key string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查是否持有该锁
	mutex, exists := l.locks[key]
	if !exists {
		return fmt.Errorf("lock not held: %s", key)
	}

	// 释放锁
	if err := mutex.Unlock(ctx); err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}

	// 从映射中删除
	delete(l.locks, key)

	return nil
}

// LockWithTTL 带TTL的加锁
func (l *EtcdLocker) LockWithTTL(ctx context.Context, key string, ttl time.Duration) error {
	// 创建临时 session，使用指定的 TTL
	session, err := concurrency.NewSession(l.client, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查是否已经持有该锁
	if _, exists := l.locks[key]; exists {
		session.Close()
		return fmt.Errorf("lock already held: %s", key)
	}

	// 创建互斥锁
	mutex := concurrency.NewMutex(session, key)

	// 阻塞式获取锁
	if err := mutex.Lock(ctx); err != nil {
		session.Close()
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// 记录已持有的锁
	l.locks[key] = mutex

	// 如果启用自动续期，启动续期协程
	if l.options.AutoRenew {
		go l.keepAlive(ctx, key, session)
	}

	return nil
}

// keepAlive 自动续期
func (l *EtcdLocker) keepAlive(ctx context.Context, key string, session *concurrency.Session) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-session.Done():
			return
		case <-time.After(l.options.TTL / 3): // 在 TTL 的 1/3 时续期
			// 检查锁是否还在持有
			l.mu.RLock()
			_, exists := l.locks[key]
			l.mu.RUnlock()
			if !exists {
				return
			}
		}
	}
}

// Close 关闭锁客户端
func (l *EtcdLocker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 释放所有持有的锁
	ctx := context.Background()
	for key, mutex := range l.locks {
		if err := mutex.Unlock(ctx); err != nil {
			// 记录错误但继续
			fmt.Printf("failed to unlock %s: %v\n", key, err)
		}
	}
	l.locks = make(map[string]*concurrency.Mutex)

	// 关闭 session
	if l.session != nil {
		return l.session.Close()
	}

	return nil
}

// NewEtcdLockerWithClient 使用现有客户端创建锁（支持连接复用）
func NewEtcdLockerWithClient(client *clientv3.Client, opts *lock.LockOptions) (*EtcdLocker, error) {
	if opts == nil {
		opts = lock.DefaultLockOptions()
	}

	// 创建session，用于租约管理
	session, err := concurrency.NewSession(client, concurrency.WithTTL(int(opts.TTL.Seconds())))
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &EtcdLocker{
		client:  client,
		session: session,
		options: opts,
		locks:   make(map[string]*concurrency.Mutex),
	}, nil
}
