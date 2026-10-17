package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/internal/testkit"
)

func (r *pausedWriteRemote) Delete(ctx context.Context, key string) error {
	close(r.entered)
	select {
	case <-r.release:
		return r.Distributed.Delete(ctx, key)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMultiSlowWriteDoesNotBlockUnrelatedL1Hit(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"Set", "Delete"} {
		t.Run(op, func(t *testing.T) {
			local, err := NewLocal(&LocalConfig{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, local.Close()) })
			require.NoError(t, local.Set(t.Context(), "hot", "cached", time.Minute))
			remote := &pausedWriteRemote{Distributed: newMockKVForMulti(), entered: make(chan struct{}), release: make(chan struct{})}
			m, err := NewMulti(local, remote, nil)
			require.NoError(t, err)
			require.NotSame(t, m.(*multiCache).gateFor("cold"), m.(*multiCache).gateFor("hot"))
			done := make(chan error, 1)
			go func() {
				if op == "Set" {
					done <- m.Set(t.Context(), "cold", "value", time.Minute)
				} else {
					done <- m.Delete(t.Context(), "cold")
				}
			}()
			<-remote.entered
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var value string
			err = m.Get(ctx, "hot", &value)
			close(remote.release)
			require.NoError(t, <-done)
			require.NoError(t, err)
			require.Equal(t, "cached", value)
		})
	}
}

func TestHGetAllReplacesReusedMap(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	c, err := NewDistributed(&DistributedConfig{}, WithRedisConnector(conn))
	require.NoError(t, err)
	require.NoError(t, c.HSet(t.Context(), "hash", "fresh", "value"))
	values := map[string]string{"stale": "old"}
	require.NoError(t, c.HGetAll(t.Context(), "hash", &values))
	require.Equal(t, map[string]string{"fresh": "value"}, values)
	require.NoError(t, c.HGetAll(t.Context(), "missing", &values))
	require.Empty(t, values)
}

func TestSliceDestinationErrorsAndEmptyMGet(t *testing.T) {
	t.Parallel()
	c := &redisCache{}
	var nilSlice *[]string
	for _, dest := range []any{nil, nilSlice, "wrong", new(string)} {
		require.ErrorIs(t, c.MGet(t.Context(), nil, dest), ErrInvalidDestination)
		require.ErrorIs(t, c.unmarshalSlice(nil, dest), ErrInvalidDestination)
	}
	values := []string{"stale"}
	require.NoError(t, c.MGet(t.Context(), nil, &values))
	require.Empty(t, values)
}
