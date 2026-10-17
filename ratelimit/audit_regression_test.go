package ratelimit

import (
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/metrics"
)

func TestRedisTimestampSerializationPreservesHighRateDebt(t *testing.T) {
	client := testkit.NewRedisContainerClient(t)
	// Freeze server time to isolate serialization precision from network latency.
	script := redis.NewScript(strings.Replace(luaScript, `redis.call("TIME")`, `{1700000000, 0}`, 1))
	allowed := 0
	for range 30 {
		result, err := script.Run(t.Context(), client, []string{"audit:precision"}, 20000, 10, 1).Slice()
		require.NoError(t, err)
		if result[0].(int64) == 1 {
			allowed++
		}
	}
	// Binary floating-point may conservatively lose one token, but must not
	// erase the accumulated debt and admit arbitrarily many requests.
	require.GreaterOrEqual(t, allowed, 9)
	require.LessOrEqual(t, allowed, 10)
}

func TestStandaloneReclaimsIdleKeysAtCapacity(t *testing.T) {
	t.Parallel()
	limiter, err := newStandalone(&StandaloneConfig{MaxKeys: 1, IdleTimeout: time.Second, CleanupInterval: time.Hour}, clog.Discard(), metrics.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, limiter.Close()) })
	l := limiter.(*standaloneLimiter)
	limit := Limit{Rate: 1, Burst: 1}
	key, wrapper, err := l.acquireLimiter("old", limit)
	require.NoError(t, err)
	l.limiterMu.Lock()
	wrapper.lastSeen = time.Now().Add(-time.Minute)
	l.limiterMu.Unlock()
	_, _, err = l.acquireLimiter("new", limit)
	require.ErrorIs(t, err, ErrKeyLimitExceeded, "active keys must not be evicted")
	l.releaseLimiter(key, wrapper)
	l.limiterMu.Lock()
	wrapper.lastSeen = time.Now().Add(-time.Minute)
	l.limiterMu.Unlock()
	newKey, replacement, err := l.acquireLimiter("new", limit)
	require.NoError(t, err)
	l.releaseLimiter(newKey, replacement)
}
