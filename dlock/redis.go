package dlock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"maps"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/xerrors"
)

type redisLocker struct {
	client *redis.Client
	cfg    *Config
	logger clog.Logger
	locks  map[string]*redisLockEntry
	lost   boundedTombstones[*redisLockEntry]
	mu     sync.RWMutex

	closeOnce sync.Once
	closeErr  error
	closed    bool
}

type redisLockEntry struct {
	key        string
	token      string
	expiration time.Duration
	renewStop  chan struct{}
	renewDone  chan struct{}
	renewOnce  sync.Once
	lostCh     chan error
	lostOnce   sync.Once
	releaseMu  sync.Mutex
}

// newRedisLocker 创建 Redis Locker 实例
func newRedis(conn connector.RedisConnector, cfg *Config, logger clog.Logger) (Locker, error) {
	if conn == nil {
		return nil, ErrConnectorNil
	}
	if cfg == nil {
		return nil, ErrConfigNil
	}

	client := conn.GetClient()
	if client == nil {
		return nil, xerrors.Wrap(ErrConnectorNil, "redis connector is not connected")
	}
	return &redisLocker{
		client: client,
		cfg:    cfg,
		logger: logger,
		locks:  make(map[string]*redisLockEntry),
		lost:   newBoundedTombstones[*redisLockEntry](),
	}, nil
}

func (l *redisLocker) Lock(ctx context.Context, key string, opts ...LockOption) error {
	return l.lockWithRetry(ctx, key, opts...)
}

func (l *redisLocker) TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error) {
	entry, err := l.acquireLock(ctx, key, opts...)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return false, nil
	}
	return true, nil
}

func (l *redisLocker) Unlock(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	l.mu.RLock()
	entry, exists := l.locks[key]
	if !exists {
		_, lost := l.lost.Get(key)
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

	l.stopWatchdog(entry)

	// 使用 Lua 脚本安全释放锁
	result, err := l.releaseEntry(ctx, key, entry)
	if err != nil {
		// Keep the local entry so the caller can retry Unlock with a new context.
		l.restartWatchdog(key, entry)
		return err
	}

	if result.(int64) == 0 {
		l.markOwnershipLost(key, entry)
		return xerrors.Wrapf(ErrOwnershipLost, "key: %s", key)
	}
	l.removeReleased(key, entry)

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock released", clog.String("key", key))
	}
	return nil
}

func (l *redisLocker) restartWatchdog(key string, entry *redisLockEntry) {
	l.mu.Lock()
	if l.closed || l.locks[key] != entry {
		l.mu.Unlock()
		return
	}
	entry.renewStop = make(chan struct{})
	entry.renewDone = make(chan struct{})
	entry.renewOnce = sync.Once{}
	l.mu.Unlock()
	go l.watchdog(entry, l.getRedisKey(key))
}

func (l *redisLocker) Lost(key string) <-chan error {
	if err := validateKey(key); err != nil {
		return terminalErrorChannel(err)
	}
	l.mu.RLock()
	entry := l.locks[key]
	if entry == nil {
		entry, _ = l.lost.Get(key)
	}
	l.mu.RUnlock()
	if entry != nil {
		return entry.lostCh
	}
	return terminalErrorChannel(xerrors.Wrapf(ErrLockNotHeld, "key: %s", key))
}

func (l *redisLocker) lockWithRetry(ctx context.Context, key string, opts ...LockOption) error {
	retryInterval := l.cfg.RetryInterval
	if retryInterval <= 0 {
		retryInterval = 100 * time.Millisecond
	}

	for {
		entry, err := l.acquireLock(ctx, key, opts...)
		if err != nil {
			return err
		}
		if entry != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
			continue
		}
	}
}

func (l *redisLocker) acquireLock(ctx context.Context, key string, opts ...LockOption) (*redisLockEntry, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	ttl, err := resolveLockTTL(l.cfg.DefaultTTL, opts...)
	if err != nil {
		return nil, err
	}
	if err := validateRedisTTL(ttl); err != nil {
		return nil, err
	}

	// 先检查本地是否已持有锁
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		return nil, xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}
	l.lost.Delete(key)
	l.mu.Unlock()

	// 生成随机 token
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		return nil, xerrors.Wrap(err, "failed to generate random token")
	}
	token := hex.EncodeToString(randBytes)
	redisKey := l.getRedisKey(key)

	success, err := l.client.SetNX(ctx, redisKey, token, ttl).Result()
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to acquire lock")
	}

	if !success {
		return nil, nil
	}
	delScript := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	// 获取 Redis 锁成功后，再次检查本地状态并添加
	// 使用双重检查避免竞态条件
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_, _ = l.client.Eval(ctx, delScript, []string{redisKey}, token).Result()
		return nil, ErrClosed
	}
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		// 本地已存在（竞态情况），释放刚获取的 Redis 锁
		_, _ = l.client.Eval(ctx, delScript, []string{redisKey}, token).Result()
		return nil, xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}

	entry := &redisLockEntry{
		key:        key,
		token:      token,
		expiration: ttl,
		renewStop:  make(chan struct{}),
		renewDone:  make(chan struct{}),
		lostCh:     make(chan error, 1),
	}

	l.locks[key] = entry
	l.lost.Delete(key)
	l.mu.Unlock()

	go l.watchdog(entry, redisKey)

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock acquired", clog.String("key", key))
	}
	return entry, nil
}

func (l *redisLocker) watchdog(entry *redisLockEntry, redisKey string) {
	defer close(entry.renewDone)

	renewInterval := max(entry.expiration/3, time.Millisecond)
	ticker := time.NewTicker(renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-entry.renewStop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			script := `
				if redis.call("GET", KEYS[1]) == ARGV[1] then
					return redis.call("PEXPIRE", KEYS[1], ARGV[2])
				else
					return 0
				end
			`
			res, err := l.client.Eval(ctx, script, []string{redisKey}, entry.token, entry.expiration.Milliseconds()).Result()
			cancel()

			if err != nil {
				if l.logger != nil {
					l.logger.Error("watchdog renew failed", clog.String("key", entry.key), clog.Error(err))
				}
				l.markOwnershipLost(entry.key, entry)
				return
			}
			if res.(int64) == 0 {
				if l.logger != nil {
					l.logger.Warn("watchdog lost ownership", clog.String("key", entry.key))
				}
				l.markOwnershipLost(entry.key, entry)
				return
			}
		}
	}
}

func (l *redisLocker) markOwnershipLost(key string, entry *redisLockEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	current, exists := l.locks[key]
	if exists && current == entry {
		delete(l.locks, key)
		l.lost.Put(key, entry)
		l.signalOwnershipLost(key, entry, nil)
	}
}

func (l *redisLocker) signalOwnershipLost(key string, entry *redisLockEntry, cause error) {
	entry.lostOnce.Do(func() {
		err := xerrors.Wrapf(ErrOwnershipLost, "key: %s", key)
		if cause != nil {
			err = xerrors.Join(err, cause)
		}
		entry.lostCh <- err
		close(entry.lostCh)
	})
}

func (l *redisLocker) removeReleased(key string, entry *redisLockEntry) {
	l.mu.Lock()
	if l.locks[key] == entry {
		delete(l.locks, key)
	}
	l.mu.Unlock()
	entry.lostOnce.Do(func() { close(entry.lostCh) })
}

func (l *redisLocker) stopWatchdog(entry *redisLockEntry) {
	if entry == nil || entry.renewStop == nil {
		return
	}
	entry.renewOnce.Do(func() {
		close(entry.renewStop)
		<-entry.renewDone
	})
}

func (l *redisLocker) releaseEntry(ctx context.Context, key string, entry *redisLockEntry) (any, error) {
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`
	redisKey := l.getRedisKey(key)
	result, err := l.client.Eval(ctx, script, []string{redisKey}, entry.token).Result()
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to release lock")
	}
	return result, nil
}

func (l *redisLocker) getRedisKey(key string) string {
	if l.cfg.Prefix != "" {
		return l.cfg.Prefix + key
	}
	return key
}

// Close 关闭 Redis Locker，停止续租并尽力释放仍持有的锁。
// 它不关闭借用的 Redis connector。
func (l *redisLocker) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		entries := make(map[string]*redisLockEntry, len(l.locks))
		maps.Copy(entries, l.locks)
		l.locks = make(map[string]*redisLockEntry)
		l.lost.Clear()
		l.mu.Unlock()

		var errs []error
		for key, entry := range entries {
			l.stopWatchdog(entry)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			result, err := l.releaseEntry(ctx, key, entry)
			cancel()
			if err != nil {
				errs = append(errs, err)
				l.signalOwnershipLost(key, entry, err)
				continue
			}
			if result.(int64) == 0 {
				lostErr := xerrors.Wrapf(ErrOwnershipLost, "key: %s", key)
				errs = append(errs, lostErr)
				l.signalOwnershipLost(key, entry, nil)
				continue
			}
			entry.lostOnce.Do(func() { close(entry.lostCh) })
		}

		l.closeErr = xerrors.Combine(errs...)
	})
	return l.closeErr
}
