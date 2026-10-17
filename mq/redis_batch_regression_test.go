package mq

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/internal/testkit"
)

type cancelFetchedBatchHook struct {
	cancel   context.CancelFunc
	commands []string
}

func (h *cancelFetchedBatchHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *cancelFetchedBatchHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *cancelFetchedBatchHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.commands = append(h.commands, cmd.Name())
		err := next(ctx, cmd)
		if cmd.Name() == "xreadgroup" && err == nil {
			h.cancel()
		}
		return err
	}
}

func TestRedisDrainFinishesFetchedBatchAndClaimsImmediately(t *testing.T) {
	conn := testkit.NewRedisContainerConnector(t)
	client := conn.GetClient()
	const topic, group = "audit-batch", "workers"
	require.NoError(t, client.XGroupCreateMkStream(t.Context(), topic, group, "0").Err())
	for range 3 {
		require.NoError(t, client.XAdd(t.Context(), &redis.XAddArgs{Stream: topic, Values: map[string]any{redisFieldPayload: "data"}}).Err())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	hook := &cancelFetchedBatchHook{cancel: cancel}
	client.AddHook(hook)
	transport := newRedisStreamTransport(conn, &RedisStreamConfig{PendingIdle: time.Second}, clog.Discard())
	var handled int
	transport.consumeWithGroup(ctx, t.Context(), topic, subscribeOptions{QueueGroup: group, DurableName: "consumer", BatchSize: 10}, func(msg Message) error {
		handled++
		return msg.Ack()
	})
	require.Equal(t, 3, handled)
	require.NotEmpty(t, hook.commands)
	require.Equal(t, "xautoclaim", hook.commands[0])
	pending, err := client.XPending(t.Context(), topic, group).Result()
	require.NoError(t, err)
	require.Zero(t, pending.Count)
}
