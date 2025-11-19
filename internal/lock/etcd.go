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
	locks   map[string]*lockEntry // 维护已持有的锁
}

// lockEntry 锁条目，包含所有相关资源
type lockEntry struct {
	mutex   *concurrency.Mutex
	session *concurrency.Session
	isTTL   bool // 标记是否为TTL锁
}

// NewEtcdLocker 创建 etcd 分布式锁实例（接收外部配置）
func NewEtcdLocker(config *connector.EtcdConfig, opts *lock.LockOptions) (*EtcdLocker, error) {
	if opts == nil {
		opts = lock.DefaultLockOptions()
	}

	// 使用外部配置或默认值
	if config == nil {
		config = &connector.EtcdConfig{
			Endpoints: []string{"127.0.0.1:2379"},
			Timeout:   5 * time.Second,
		}
	}

	// 从连接管理器获取 etcd 客户端
	manager := connector.GetEtcdManager()
	client, err := manager.GetEtcdClient(*config)
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
		locks:   make(map[string]*lockEntry),
	}, nil
}

// Lock 阻塞式加锁
func (l *EtcdLocker) Lock(ctx context.Context, key string) error {
	// 第一步：快速检查（无锁）
	l.mu.RLock()
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return fmt.Errorf("lock already held: %s", key)
	}
	l.mu.RUnlock()

	// 第二步：获取 etcd 锁（不持有本地锁）
	mutex := concurrency.NewMutex(l.session, key)
	if err := mutex.Lock(ctx); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// 第三步：最终确认（持有本地锁）
	l.mu.Lock()
	defer l.mu.Unlock()

	// 双重检查，避免并发重复创建
	if _, exists := l.locks[key]; exists {
		// 回滚：释放刚获取的 etcd 锁
		mutex.Unlock(ctx)
		return fmt.Errorf("lock already held: %s", key)
	}

	// 记录已持有的锁
	l.locks[key] = &lockEntry{
		mutex:   mutex,
		session: l.session,
		isTTL:   false,
	}

	return nil
}

// TryLock 非阻塞式加锁
func (l *EtcdLocker) TryLock(ctx context.Context, key string) (bool, error) {
	// 第一步：快速检查（无锁）
	l.mu.RLock()
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return false, fmt.Errorf("lock already held: %s", key)
	}
	l.mu.RUnlock()

	// 第二步：尝试获取 etcd 锁（不持有本地锁）
	mutex := concurrency.NewMutex(l.session, key)
	if err := mutex.TryLock(ctx); err != nil {
		if err == concurrency.ErrLocked {
			return false, nil
		}
		return false, fmt.Errorf("failed to try lock: %w", err)
	}

	// 第三步：最终确认（持有本地锁）
	l.mu.Lock()
	defer l.mu.Unlock()

	// 双重检查
	if _, exists := l.locks[key]; exists {
		// 回滚：释放刚获取的 etcd 锁
		mutex.Unlock(ctx)
		return false, fmt.Errorf("lock already held: %s", key)
	}

	// 记录已持有的锁
	l.locks[key] = &lockEntry{
		mutex:   mutex,
		session: l.session,
		isTTL:   false,
	}

	return true, nil
}

// Unlock 释放锁
func (l *EtcdLocker) Unlock(ctx context.Context, key string) error {
	// 先获取锁条目，然后立即释放本地锁
	l.mu.Lock()
	entry, exists := l.locks[key]
	if !exists {
		l.mu.Unlock()
		return fmt.Errorf("lock not held: %s", key)
	}
	// 从映射中删除，避免重复解锁
	delete(l.locks, key)
	l.mu.Unlock()

	// 释放 etcd 锁（不持有本地锁）
	if err := entry.mutex.Unlock(ctx); err != nil {
		return fmt.Errorf("failed to unlock: %w", err)
	}

	// 如果是TTL锁且有独立session，关闭session
	if entry.isTTL && entry.session != l.session {
		entry.session.Close()
	}

	return nil
}

// LockWithTTL 带TTL的加锁
func (l *EtcdLocker) LockWithTTL(ctx context.Context, key string, ttl time.Duration) error {
	// 第一步：快速检查（无锁）
	l.mu.RLock()
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return fmt.Errorf("lock already held: %s", key)
	}
	l.mu.RUnlock()

	// 创建临时 session，使用指定的 TTL
	session, err := concurrency.NewSession(l.client, concurrency.WithTTL(int(ttl.Seconds())))
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	// 创建互斥锁
	mutex := concurrency.NewMutex(session, key)

	// 阻塞式获取锁
	if err := mutex.Lock(ctx); err != nil {
		session.Close()
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// 最终确认（持有本地锁）
	l.mu.Lock()
	defer l.mu.Unlock()

	// 双重检查
	if _, exists := l.locks[key]; exists {
		// 回滚：释放刚获取的 etcd 锁和session
		mutex.Unlock(ctx)
		session.Close()
		return fmt.Errorf("lock already held: %s", key)
	}

	// 记录已持有的锁
	l.locks[key] = &lockEntry{
		mutex:   mutex,
		session: session,
		isTTL:   true,
	}

	return nil
}

// 移除无效的 keepAlive 函数，直接依赖 etcd session 的内置续期机制

// Close 关闭锁客户端
func (l *EtcdLocker) Close() error {
	l.mu.Lock()
	entries := make([]*lockEntry, 0, len(l.locks))
	for _, entry := range l.locks {
		entries = append(entries, entry)
	}
	// 清空映射，避免重复操作
	l.locks = make(map[string]*lockEntry)
	l.mu.Unlock()

	// 释放所有持有的锁
	ctx := context.Background()
	for _, entry := range entries {
		if err := entry.mutex.Unlock(ctx); err != nil {
			// 记录错误但继续
			fmt.Printf("failed to unlock: %v\n", err)
		}
		// 关闭独立session（TTL锁）
		if entry.isTTL && entry.session != l.session {
			entry.session.Close()
		}
	}

	// 关闭默认 session
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
		locks:   make(map[string]*lockEntry),
	}, nil
}
