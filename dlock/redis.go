package dlock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	mu     sync.RWMutex
}

type redisLockEntry struct {
	key        string
	token      string
	expiration time.Duration
	renewStop  chan struct{}
	renewDone  chan struct{}
}

// newRedisLocker 创建 Redis Locker 实例
func newRedis(conn connector.RedisConnector, cfg *Config, logger clog.Logger) (Locker, error) {
	if conn == nil {
		return nil, ErrConnectorNil
	}
	if cfg == nil {
		return nil, ErrConfigNil
	}

	return &redisLocker{
		client: conn.GetClient(),
		cfg:    cfg,
		logger: logger,
		locks:  make(map[string]*redisLockEntry),
	}, nil
}

func (l *redisLocker) Lock(ctx context.Context, key string, opts ...LockOption) error {
	return l.lockWithRetry(ctx, key, false, opts...)
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
	l.mu.Lock()
	entry, exists := l.locks[key]
	if !exists {
		l.mu.Unlock()
		return xerrors.Wrapf(ErrLockNotHeld, "key: %s", key)
	}
	delete(l.locks, key)
	l.mu.Unlock()

	// 停止续约
	if entry.renewStop != nil {
		close(entry.renewStop)
		<-entry.renewDone
	}

	// 使用 Lua 脚本安全释放锁
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
		return xerrors.Wrap(err, "failed to release lock")
	}

	if result.(int64) == 0 {
		return xerrors.Wrapf(ErrOwnershipLost, "key: %s", key)
	}

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock released", clog.String("key", key))
	}
	return nil
}

func (l *redisLocker) lockWithRetry(ctx context.Context, key string, tryOnce bool, opts ...LockOption) error {
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

		if tryOnce {
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
	options := &lockOptions{
		TTL: l.cfg.DefaultTTL,
	}
	for _, opt := range opts {
		opt(options)
	}
	if options.TTL <= 0 {
		options.TTL = 10 * time.Second
	}

	// 先检查本地是否已持有锁
	l.mu.Lock()
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		return nil, xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}
	l.mu.Unlock()

	// 生成随机 token
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		return nil, xerrors.Wrap(err, "failed to generate random token")
	}
	token := hex.EncodeToString(randBytes)
	redisKey := l.getRedisKey(key)

	success, err := l.client.SetNX(ctx, redisKey, token, options.TTL).Result()
	if err != nil {
		return nil, xerrors.Wrap(err, "failed to acquire lock")
	}

	if !success {
		return nil, nil
	}

	// 获取 Redis 锁成功后，再次检查本地状态并添加
	// 使用双重检查避免竞态条件
	l.mu.Lock()
	if _, exists := l.locks[key]; exists {
		l.mu.Unlock()
		// 本地已存在（竞态情况），释放刚获取的 Redis 锁
		delScript := `
			if redis.call("GET", KEYS[1]) == ARGV[1] then
				return redis.call("DEL", KEYS[1])
			else
				return 0
			end
		`
		_, _ = l.client.Eval(ctx, delScript, []string{redisKey}, token).Result()
		return nil, xerrors.Wrapf(ErrLockAlreadyHeld, "key: %s", key)
	}

	entry := &redisLockEntry{
		key:        key,
		token:      token,
		expiration: options.TTL,
		renewStop:  make(chan struct{}),
		renewDone:  make(chan struct{}),
	}

	l.locks[key] = entry
	l.mu.Unlock()

	go l.watchdog(entry, redisKey)

	if l.logger != nil {
		l.logger.InfoContext(ctx, "lock acquired", clog.String("key", key), clog.String("token", token))
	}
	return entry, nil
}

func (l *redisLocker) watchdog(entry *redisLockEntry, redisKey string) {
	defer close(entry.renewDone)

	renewInterval := entry.expiration / 3
	if renewInterval < time.Second {
		renewInterval = time.Second
	}
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
				return
			}
			if res.(int64) == 0 {
				if l.logger != nil {
					l.logger.Warn("watchdog lost ownership", clog.String("key", entry.key))
				}
				return
			}
		}
	}
}

func (l *redisLocker) getRedisKey(key string) string {
	if l.cfg.Prefix != "" {
		return l.cfg.Prefix + key
	}
	return key
}

// Close 关闭 Redis Locker
// Redis Locker 不拥有底层连接，因此是 no-op
func (l *redisLocker) Close() error {
	return nil
}
