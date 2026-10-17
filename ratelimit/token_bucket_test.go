package ratelimit

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/metrics"
)

func frozenLimiterScript(seconds, micros int64) *redis.Script {
	clock := fmt.Sprintf("{%d, %d}", seconds, micros)
	return redis.NewScript(strings.Replace(luaScript, `redis.call("TIME")`, clock, 1))
}

func TestDistributedOversizedRequestNeverPasses(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	limiter, err := newDistributed(&DistributedConfig{}, conn, clog.Discard(), metrics.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, limiter.Close()) })
	allowed, err := limiter.AllowN(t.Context(), "oversized", Limit{Rate: 1e9, Burst: 1}, 2)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestRedisFrozenClockCannotRefillBucket(t *testing.T) {
	client := testkit.NewRedisContainerClient(t)
	for _, rate := range []float64{0.5, 10, 20000, 1e6, 1e9} {
		t.Run(fmt.Sprint(rate), func(t *testing.T) {
			key := "frozen:" + t.Name()
			script := frozenLimiterScript(1700000000, 123456)
			allowed := 0
			for range 30 {
				result, err := script.Run(t.Context(), client, []string{key}, rate, 10, 1).Slice()
				require.NoError(t, err)
				if result[0].(int64) == 1 {
					allowed++
				}
			}
			require.Equal(t, 10, allowed, "unchanged server time cannot refill a bucket")
			ttl, err := client.TTL(t.Context(), key).Result()
			require.NoError(t, err)
			require.Positive(t, ttl)
		})
	}
}

func TestRedisTokenBucketKeepsOriginalKeyAndConvertsLegacyBalance(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	limiter, err := newDistributed(&DistributedConfig{}, conn, clog.Discard(), metrics.Discard())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, limiter.Close()) })
	impl := limiter.(*distributedLimiter)
	key := impl.buildKey("legacy", Limit{Rate: 10, Burst: 10})
	require.Equal(t, impl.prefix+"legacy:rate=10:burst=10", key)
	client := conn.GetClient()
	// At 10 tokens/s, 0.5s of future TAT represents 5 consumed tokens.
	require.NoError(t, client.Set(t.Context(), key, "1700000000.5", 0).Err())
	script := frozenLimiterScript(1700000000, 0)
	result, err := script.Run(t.Context(), client, []string{key}, 10, 10, 6).Slice()
	require.NoError(t, err)
	require.EqualValues(t, 0, result[0], "conversion must not reset the bucket to full")
	require.Equal(t, "5,1700000000000000", client.Get(t.Context(), key).Val())
	require.Equal(t, "string", client.Type(t.Context(), key).Val())
	result, err = script.Run(t.Context(), client, []string{key}, 10, 10, 5).Slice()
	require.NoError(t, err)
	require.EqualValues(t, 1, result[0])
	require.Equal(t, "0,1700000000000000", client.Get(t.Context(), key).Val())
}

func TestRedisTokenBucketRejectsInvalidStoredState(t *testing.T) {
	client := testkit.NewRedisContainerClient(t)
	for _, state := range []string{"broken", "1,broken"} {
		require.NoError(t, client.Set(t.Context(), "invalid-state", state, 0).Err())
		_, err := frozenLimiterScript(1700000000, 0).Run(t.Context(), client, []string{"invalid-state"}, 10, 10, 1).Result()
		require.Error(t, err)
		require.Equal(t, state, client.Get(t.Context(), "invalid-state").Val(), "invalid state must not be silently reset")
	}
}

func TestDistributedLimitNumericBounds(t *testing.T) {
	t.Parallel()
	for _, limit := range []Limit{
		{Rate: math.NaN(), Burst: 1},
		{Rate: math.Inf(1), Burst: 1},
		{Rate: 1e-300, Burst: 1},
		{Rate: 1, Burst: int(maxRedisExactInteger + 1)},
	} {
		require.False(t, validRedisLimit(limit))
		_, err := (&distributedLimiter{}).AllowN(t.Context(), "invalid", limit, 1)
		require.ErrorIs(t, err, ErrInvalidLimit)
	}
	require.True(t, validRedisLimit(Limit{Rate: 1e9, Burst: 1}))
}

func TestRedisTokenBucketRefillAndClockRollback(t *testing.T) {
	client := testkit.NewRedisContainerClient(t)
	call := func(key string, sec, usec int64, rate float64, burst, requested int) bool {
		result, err := frozenLimiterScript(sec, usec).Run(t.Context(), client, []string{key}, rate, burst, requested).Slice()
		require.NoError(t, err)
		return result[0].(int64) == 1
	}
	const now = int64(1700000000)
	// Ordinary bucket: 5 -> spend 3 -> 2; after 200ms at 10/s -> 4.
	require.True(t, call("ordinary", now, 0, 10, 5, 3))
	require.False(t, call("ordinary", now, 0, 10, 5, 3))
	require.True(t, call("ordinary", now, 200000, 10, 5, 3))
	require.False(t, call("ordinary", now, 200000, 10, 5, 2))
	// Idle refill is capped at Burst, not accumulated without a ceiling.
	require.True(t, call("ordinary", now+100, 0, 10, 5, 5))
	require.False(t, call("ordinary", now+100, 0, 10, 5, 1))

	// Fractional refill survives both rejected and successful requests.
	require.True(t, call("fractional", now, 0, 0.5, 2, 2))
	require.False(t, call("fractional", now+1, 0, 0.5, 2, 1))
	require.True(t, call("fractional", now+2, 0, 0.5, 2, 1))
	require.False(t, call("fractional", now+2, 0, 0.5, 2, 1))
	require.True(t, call("fractional-spend", now, 0, 0.5, 10, 1))
	require.True(t, call("fractional-spend", now+1, 0, 0.5, 10, 1))
	require.True(t, call("fractional-spend", now+2, 0, 0.5, 10, 1))
	require.True(t, call("fractional-spend", now+2, 0, 0.5, 10, 8))
	require.False(t, call("fractional-spend", now+2, 0, 0.5, 10, 1))

	require.True(t, call("high-rate", now, 123456, 1e9, 1, 1))
	require.False(t, call("high-rate", now, 123456, 1e9, 1, 1))
	require.True(t, call("high-rate", now, 123457, 1e9, 1, 1))
	require.False(t, call("high-rate", now, 123457, 1e9, 1, 1))

	require.True(t, call("rollback", now, 0, 1, 1, 1))
	require.False(t, call("rollback", now-1, 0, 1, 1, 1))
	require.False(t, call("rollback", now, 0, 1, 1, 1))
	require.True(t, call("rollback", now+1, 0, 1, 1, 1))
}
