// internal/lock/redis.go
package lock

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/lock"
	"github.com/redis/go-redis/v9"
)

// RedisLocker 基于Redis的分布式锁实现
// 使用Redis的SET命令实现分布式锁，支持锁续约和自动过期
type RedisLocker struct {
	client  *redis.Client              // Redis客户端
	options *lock.LockOptions          // 锁配置选项
	locks   map[string]*redisLockEntry // 锁名称 -> 锁条目
	mu      sync.RWMutex               // 保护locks map的并发安全
}

// redisLockEntry Redis锁条目
// 包含锁的所有状态信息和续约机制
type redisLockEntry struct {
	key        string        // Redis中的锁键
	token      string        // 锁的唯一标识符
	expiration time.Duration // 锁的过期时间
	isLocked   bool          // 是否已加锁
	autoRenew  bool          // 是否自动续约
	renewStop  chan struct{} // 停止续约的信号通道
	renewDone  chan struct{} // 续约goroutine完成的信号
	createdAt  time.Time     // 锁创建时间
}

// NewRedisLockerWithClient 使用现有Redis客户端创建锁（支持连接复用）
// 这是推荐的使用方式，可以充分利用连接管理器的复用功能
func NewRedisLockerWithClient(client *redis.Client, opts *lock.LockOptions) (*RedisLocker, error) {
	if client == nil {
		return nil, fmt.Errorf("Redis客户端不能为空")
	}

	if opts == nil {
		opts = lock.DefaultLockOptions()
	}

	return &RedisLocker{
		client:  client,
		options: opts,
		locks:   make(map[string]*redisLockEntry),
	}, nil
}

// Lock 获取分布式锁（阻塞模式）
// 如果锁已被其他客户端持有，会按照RetryInterval间隔重试
// 直到获取锁成功或上下文被取消
func (l *RedisLocker) Lock(ctx context.Context, key string) error {
	return l.lockWithRetry(ctx, key, false)
}

// TryLock 尝试获取分布式锁（非阻塞模式）
// 如果锁已被其他客户端持有，立即返回失败
// 返回值：成功获取锁返回true，否则返回false
func (l *RedisLocker) TryLock(ctx context.Context, key string) (bool, error) {
	entry, err := l.acquireLock(ctx, key)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return false, nil // 锁被占用
	}
	return true, nil
}

// LockWithTTL 带TTL的加锁，自动续期
// 使用指定的TTL时间加锁，而不是使用默认的options.TTL
func (l *RedisLocker) LockWithTTL(ctx context.Context, key string, ttl time.Duration) error {
	// 保存原始TTL
	originalTTL := l.options.TTL
	// 设置新的TTL
	l.options.TTL = ttl

	// 执行加锁
	err := l.Lock(ctx, key)

	// 恢复原始TTL
	l.options.TTL = originalTTL

	return err
}

// Unlock 释放分布式锁
// 只有锁的持有者才能成功释放锁
func (l *RedisLocker) Unlock(ctx context.Context, key string) error {
	l.mu.Lock()
	entry, exists := l.locks[key]
	if !exists {
		l.mu.Unlock()
		return fmt.Errorf("锁不存在: %s", key)
	}
	l.mu.Unlock()

	// 停止续约（如果正在运行）
	if entry.autoRenew {
		close(entry.renewStop)
		<-entry.renewDone // 等待续约goroutine完成
	}

	// 使用Lua脚本确保原子性：只有token匹配时才删除锁
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, script, []string{entry.key}, entry.token).Result()
	if err != nil {
		return fmt.Errorf("释放锁失败: %w", err)
	}

	if result.(int64) == 0 {
		return fmt.Errorf("锁释放失败（可能已被其他客户端持有）: %s", key)
	}

	// 从本地缓存中移除
	l.mu.Lock()
	delete(l.locks, key)
	l.mu.Unlock()

	log.Printf("[RedisLocker] 锁已释放: %s", key)
	return nil
}

// Close 关闭锁管理器
// 释放所有持有的锁并清理资源
func (l *RedisLocker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	var lastErr error
	for key, entry := range l.locks {
		log.Printf("[RedisLocker] 强制释放锁（关闭管理器）: %s", key)

		// 停止续约
		if entry.autoRenew {
			close(entry.renewStop)
			<-entry.renewDone
		}

		// 尝试释放锁（忽略错误）
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		script := `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`
		if err := l.client.Eval(ctx, script, []string{entry.key}, entry.token).Err(); err != nil {
			lastErr = err
		}
		cancel()
	}

	// 清空锁缓存
	l.locks = make(map[string]*redisLockEntry)
	return lastErr
}

// lockWithRetry 带重试机制的加锁
// tryOnce: true表示只尝试一次（TryLock），false表示会重试（Lock）
func (l *RedisLocker) lockWithRetry(ctx context.Context, key string, tryOnce bool) error {
	for {
		entry, err := l.acquireLock(ctx, key)
		if err != nil {
			return err
		}
		if entry != nil {
			return nil // 成功获取锁
		}

		// 锁被占用
		if tryOnce {
			return nil // TryLock模式，直接返回
		}

		// Lock模式，等待后重试
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(l.options.RetryInterval):
			continue // 重试
		}
	}
}

// acquireLock 尝试获取锁
// 返回值：成功获取锁返回锁条目，锁被占用返回nil，错误返回错误
func (l *RedisLocker) acquireLock(ctx context.Context, key string) (*redisLockEntry, error) {
	// 检查是否已经持有该锁
	l.mu.RLock()
	if existingEntry, exists := l.locks[key]; exists && existingEntry.isLocked {
		l.mu.RUnlock()
		return nil, fmt.Errorf("已经持有该锁: %s", key)
	}
	l.mu.RUnlock()

	// 生成唯一的锁token
	token := fmt.Sprintf("%s-%d", key, time.Now().UnixNano())

	// 构建Redis锁键
	redisKey := fmt.Sprintf("lock:%s", key)

	// 使用SET命令获取锁（NX: 不存在时才设置，PX: 设置过期时间）
	success, err := l.client.SetNX(ctx, redisKey, token, l.options.TTL).Result()
	if err != nil {
		return nil, fmt.Errorf("获取锁失败: %w", err)
	}

	if !success {
		// 锁被其他客户端持有
		return nil, nil
	}

	// 成功获取锁，创建锁条目
	entry := &redisLockEntry{
		key:        redisKey,
		token:      token,
		expiration: l.options.TTL,
		isLocked:   true,
		autoRenew:  l.options.AutoRenew,
		createdAt:  time.Now(),
	}

	// 如果需要自动续约，启动续约goroutine
	if entry.autoRenew {
		entry.renewStop = make(chan struct{})
		entry.renewDone = make(chan struct{})
		go l.autoRenewLoop(entry)
	}

	// 缓存到本地
	l.mu.Lock()
	l.locks[key] = entry
	l.mu.Unlock()

	log.Printf("[RedisLocker] 成功获取锁: %s, token=%s", key, token)
	return entry, nil
}

// autoRenewLoop 自动续约循环
// 定期续约锁，防止锁过期
func (l *RedisLocker) autoRenewLoop(entry *redisLockEntry) {
	defer close(entry.renewDone)

	// 续约间隔为TTL的1/3，确保在过期前续约
	renewInterval := entry.expiration / 3
	if renewInterval < time.Second {
		renewInterval = time.Second // 最小间隔1秒
	}

	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 执行续约
			if err := l.renewLock(entry); err != nil {
				log.Printf("[RedisLocker] 锁续约失败: %s, 错误: %v", entry.key, err)
				return // 续约失败，停止续约
			}

		case <-entry.renewStop:
			log.Printf("[RedisLocker] 停止锁续约: %s", entry.key)
			return // 收到停止信号
		}
	}
}

// renewLock 续约锁
// 使用Lua脚本确保原子性：只有token匹配时才延长过期时间
func (l *RedisLocker) renewLock(entry *redisLockEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	// 将续约时间转换为毫秒
	expireMillis := entry.expiration.Milliseconds()
	result, err := l.client.Eval(ctx, script, []string{entry.key}, entry.token, expireMillis).Result()
	if err != nil {
		return fmt.Errorf("续约失败: %w", err)
	}

	if result.(int64) == 0 {
		return fmt.Errorf("续约失败（锁可能已被其他客户端持有）")
	}

	log.Printf("[RedisLocker] 锁续约成功: %s", entry.key)
	return nil
}

// GetStats 获取锁管理器统计信息
func (l *RedisLocker) GetStats() map[string]interface{} {
	l.mu.RLock()
	defer l.mu.RUnlock()

	stats := map[string]interface{}{
		"总锁数": len(l.locks),
		"锁列表": []map[string]interface{}{},
	}

	for key, entry := range l.locks {
		lockInfo := map[string]interface{}{
			"锁键":     key,
			"Redis键": entry.key,
			"Token":  entry.token,
			"过期时间":   entry.expiration.String(),
			"是否加锁":   entry.isLocked,
			"自动续约":   entry.autoRenew,
			"创建时间":   entry.createdAt,
		}
		stats["锁列表"] = append(stats["锁列表"].([]map[string]interface{}), lockInfo)
	}

	return stats
}
