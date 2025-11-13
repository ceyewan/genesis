package lock

import (
	"context"
	"time"
)

// LockGuard represents an acquired lock that can be released.
type LockGuard interface {
	// Unlock releases the lock.
	Unlock(ctx context.Context) error
	// Token returns the unique token assigned to this lock.
	Token() string
}

// Locker defines the interface for distributed locking operations.
type Locker interface {
	// TryLock attempts to acquire the lock without blocking.
	// Returns (guard, true, nil) if successful; (nil, false, nil) if lock is held by another; (nil, false, err) on error.
	TryLock(ctx context.Context, key string, opts ...Option) (LockGuard, bool, error)

	// Lock acquires the lock, blocking until successful or context cancels.
	Lock(ctx context.Context, key string, opts ...Option) (LockGuard, error)

	// Sync flushes any pending operations (e.g., lease renewals). Should be called on shutdown.
	Sync() error
}

// Option is a functional option for lock configuration.
type Option interface{}

// WithTTL specifies the time-to-live for the lock.
type WithTTL struct {
	Duration time.Duration
}

// WithTimeout specifies how long to wait when blocking on Lock.
type WithTimeout struct {
	Duration time.Duration
}

// WithWaitStrategy specifies the retry/backoff strategy for Lock attempts.
type WithWaitStrategy struct {
	Strategy WaitStrategy
}

// WaitStrategy defines how to wait between lock acquisition attempts.
type WaitStrategy interface {
	// NextWait returns the duration to wait before the next attempt.
	// attempts is zero-indexed.
	NextWait(attempts int) (time.Duration, bool) // (duration, shouldRetry)
}

// ExponentialBackoff is a basic exponential backoff wait strategy.
type ExponentialBackoff struct {
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
	MaxAttempts  int
}

func (e *ExponentialBackoff) NextWait(attempts int) (time.Duration, bool) {
	if e.MaxAttempts > 0 && attempts >= e.MaxAttempts {
		return 0, false
	}
	delay := time.Duration(float64(e.InitialDelay) * (e.Multiplier * float64(attempts)))
	if delay > e.MaxDelay {
		delay = e.MaxDelay
	}
	return delay, true
}

// LinearBackoff is a linear backoff wait strategy.
type LinearBackoff struct {
	Interval    time.Duration
	MaxAttempts int
}

func (l *LinearBackoff) NextWait(attempts int) (time.Duration, bool) {
	if l.MaxAttempts > 0 && attempts >= l.MaxAttempts {
		return 0, false
	}
	return l.Interval, true
}
