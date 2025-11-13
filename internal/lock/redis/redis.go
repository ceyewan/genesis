package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/pkg/lock"
	"github.com/redis/go-redis/v9"
)

// Config holds configuration for the Redis locker.
type Config struct {
	Addr       string // e.g., "localhost:6379"
	DB         int
	Password   string
	DefaultTTL time.Duration
}

// Locker is the Redis-based implementation of lock.Locker.
type Locker struct {
	client     *redis.Client
	defaultTTL time.Duration
}

// New creates a new Redis-based locker.
func New(cfg Config) (*Locker, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		DB:       cfg.DB,
		Password: cfg.Password,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 30 * time.Second
	}

	return &Locker{
		client:     client,
		defaultTTL: cfg.DefaultTTL,
	}, nil
}

// TryLock attempts to acquire the lock without blocking.
func (l *Locker) TryLock(ctx context.Context, key string, opts ...lock.Option) (lock.LockGuard, bool, error) {
	ttl := l.defaultTTL
	for _, opt := range opts {
		if withTTL, ok := opt.(lock.WithTTL); ok {
			ttl = withTTL.Duration
		}
	}

	token, err := generateToken()
	if err != nil {
		return nil, false, fmt.Errorf("failed to generate token: %w", err)
	}

	ok, err := l.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !ok {
		return nil, false, nil
	}

	return &Guard{
		locker: l,
		key:    key,
		token:  token,
	}, true, nil
}

// Lock acquires the lock, blocking until successful or context cancels.
func (l *Locker) Lock(ctx context.Context, key string, opts ...lock.Option) (lock.LockGuard, error) {
	ttl := l.defaultTTL
	timeout := 30 * time.Second
	waitStrategy := &lock.LinearBackoff{Interval: 10 * time.Millisecond, MaxAttempts: 0}

	for _, opt := range opts {
		switch v := opt.(type) {
		case lock.WithTTL:
			ttl = v.Duration
		case lock.WithTimeout:
			timeout = v.Duration
		case lock.WithWaitStrategy:
		}
	}

	deadline := time.Now().Add(timeout)
	attempts := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("lock acquisition timeout for key %q", key)
		}

		guard, ok, err := l.TryLock(ctx, key, lock.WithTTL{Duration: ttl})
		if err != nil {
			return nil, err
		}
		if ok {
			return guard, nil
		}

		wait, shouldRetry := waitStrategy.NextWait(attempts)
		if !shouldRetry {
			return nil, fmt.Errorf("max lock acquisition attempts reached for key %q", key)
		}

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		attempts++
	}
}

// Sync flushes any pending operations.
func (l *Locker) Sync() error {
	return l.client.Close()
}

// Guard represents an acquired Redis lock.
type Guard struct {
	locker *Locker
	key    string
	token  string
}

// Unlock releases the lock using a Lua script to ensure atomic check-and-delete.
func (g *Guard) Unlock(ctx context.Context) error {
	script := redis.NewScript(`
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`)
	return script.Run(ctx, g.locker.client, []string{g.key}, g.token).Err()
}

// Token returns the unique token assigned to this lock.
func (g *Guard) Token() string {
	return g.token
}

func generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
