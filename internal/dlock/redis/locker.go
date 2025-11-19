package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/ceyewan/genesis/pkg/clog"
	"github.com/ceyewan/genesis/pkg/connector"
	"github.com/ceyewan/genesis/pkg/dlock/types"
	"github.com/redis/go-redis/v9"
)

type RedisLocker struct {
	client *redis.Client
	cfg    *types.Config
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

// New 创建 RedisLocker 实例
func New(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger) (*RedisLocker, error) {
	if conn == nil {
		return nil, fmt.Errorf("redis connector is nil")
	}
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	return &RedisLocker{
		client: conn.GetClient(),
		cfg:    cfg,
		logger: logger,
		locks:  make(map[string]*redisLockEntry),
	}, nil
}

func (l *RedisLocker) Lock(ctx context.Context, key string, opts ...types.Option) error {
	return l.lockWithRetry(ctx, key, false, opts...)
}

func (l *RedisLocker) TryLock(ctx context.Context, key string, opts ...types.Option) (bool, error) {
	entry, err := l.acquireLock(ctx, key, opts...)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return false, nil
	}
	return true, nil
}

func (l *RedisLocker) Unlock(ctx context.Context, key string) error {
	l.mu.Lock()
	entry, exists := l.locks[key]
	if !exists {
		l.mu.Unlock()
		return fmt.Errorf("lock not held: %s", key)
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
		return fmt.Errorf("failed to release lock: %w", err)
	}

	if result.(int64) == 0 {
		return fmt.Errorf("failed to release lock (ownership lost): %s", key)
	}

	l.logger.InfoContext(ctx, "lock released", clog.String("key", key))
	return nil
}

func (l *RedisLocker) lockWithRetry(ctx context.Context, key string, tryOnce bool, opts ...types.Option) error {
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

func (l *RedisLocker) acquireLock(ctx context.Context, key string, opts ...types.Option) (*redisLockEntry, error) {
	l.mu.RLock()
	if _, exists := l.locks[key]; exists {
		l.mu.RUnlock()
		return nil, fmt.Errorf("lock already held locally: %s", key)
	}
	l.mu.RUnlock()

	options := &types.LockOption{
		TTL: l.cfg.DefaultTTL,
	}
	for _, opt := range opts {
		opt(options)
	}
	if options.TTL <= 0 {
		options.TTL = 10 * time.Second
	}

	// 生成随机 token
	randBytes := make([]byte, 16)
	if _, err := rand.Read(randBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random token: %w", err)
	}
	token := hex.EncodeToString(randBytes)
	redisKey := l.getRedisKey(key)

	success, err := l.client.SetNX(ctx, redisKey, token, options.TTL).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !success {
		return nil, nil
	}

	entry := &redisLockEntry{
		key:        key,
		token:      token,
		expiration: options.TTL,
		renewStop:  make(chan struct{}),
		renewDone:  make(chan struct{}),
	}

	go l.watchdog(entry, redisKey)

	l.mu.Lock()
	l.locks[key] = entry
	l.mu.Unlock()

	l.logger.InfoContext(ctx, "lock acquired", clog.String("key", key), clog.String("token", token))
	return entry, nil
}

func (l *RedisLocker) watchdog(entry *redisLockEntry, redisKey string) {
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
				l.logger.Error("watchdog renew failed", clog.String("key", entry.key), clog.Error(err))
				return
			}
			if res.(int64) == 0 {
				l.logger.Warn("watchdog lost ownership", clog.String("key", entry.key))
				return
			}
		}
	}
}

func (l *RedisLocker) getRedisKey(key string) string {
	if l.cfg.Prefix != "" {
		return l.cfg.Prefix + key
	}
	return key
}
