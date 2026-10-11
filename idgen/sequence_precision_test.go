package idgen

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/internal/testkit"
)

func TestSequencerExactInt64(t *testing.T) {
	t.Parallel()
	conn := testkit.NewRedisContainerConnector(t)
	ctx := context.Background()
	for _, start := range []int64{1<<53 - 2, 1 << 53, math.MaxInt64 - 3} {
		t.Run(strconv.FormatInt(start, 10), func(t *testing.T) {
			key := testkit.NewID()
			seq, err := NewSequencer(&SequencerConfig{TTL: time.Minute}, WithRedisConnector(conn))
			require.NoError(t, err)
			require.NoError(t, seq.Set(ctx, key, start))
			value, err := seq.Next(ctx, key)
			require.NoError(t, err)
			require.Equal(t, start+1, value)
			batch, err := seq.NextBatch(ctx, key, 2)
			require.NoError(t, err)
			require.Equal(t, []int64{start + 2, start + 3}, batch)
			stored, err := conn.GetClient().Get(ctx, key).Int64()
			require.NoError(t, err)
			require.Equal(t, start+3, stored)
		})
	}
}

func TestSequencerExhaustionPreservesValueAndTTL(t *testing.T) {
	t.Parallel()
	conn := testkit.NewRedisContainerConnector(t)
	ctx := context.Background()
	for _, maximum := range []int64{1 << 53, 1<<53 + 1, math.MaxInt64} {
		for _, batch := range []bool{false, true} {
			key := testkit.NewID()
			seq, err := NewSequencer(&SequencerConfig{MaxValue: maximum, TTL: time.Minute}, WithRedisConnector(conn))
			require.NoError(t, err)
			require.NoError(t, seq.Set(ctx, key, maximum))
			// 原 TTL 远小于配置 TTL，失败不能刷新它。
			require.NoError(t, conn.GetClient().PExpire(ctx, key, 10*time.Second).Err())
			if batch {
				_, err = seq.NextBatch(ctx, key, 2)
			} else {
				_, err = seq.Next(ctx, key)
			}
			require.ErrorIs(t, err, ErrSequenceExhausted)
			stored, err := conn.GetClient().Get(ctx, key).Int64()
			require.NoError(t, err)
			require.Equal(t, maximum, stored)
			ttl, err := conn.GetClient().PTTL(ctx, key).Result()
			require.NoError(t, err)
			require.Positive(t, ttl)
			require.LessOrEqual(t, ttl, 10*time.Second)
		}
	}
	seq, err := NewSequencer(&SequencerConfig{Step: math.MaxInt64}, WithRedisConnector(conn))
	require.NoError(t, err)
	_, err = seq.NextBatch(ctx, "overflow", 2)
	require.ErrorIs(t, err, ErrSequenceExhausted)
	require.Zero(t, conn.GetClient().Exists(ctx, "overflow").Val())
}
