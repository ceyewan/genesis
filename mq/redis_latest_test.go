package mq

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/internal/testkit"
)

// gapPublishHook 在一次空读结束和下一次读取开始之间确定性地写入消息。
type gapPublishHook struct {
	client   *redis.Client
	topic    string
	inserted atomic.Bool
}

func (h *gapPublishHook) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *gapPublishHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func (h *gapPublishHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		err := next(ctx, cmd)
		if cmd.Name() == "xread" && err == redis.Nil && h.inserted.CompareAndSwap(false, true) {
			if publishErr := h.client.XAdd(ctx, &redis.XAddArgs{Stream: h.topic, Values: map[string]any{redisFieldPayload: "gap"}}).Err(); publishErr != nil {
				return publishErr
			}
		}
		return err
	}
}

func TestRedisBroadcastLatestKeepsCursorAfterEmptyRead(t *testing.T) {
	t.Parallel()
	conn := testkit.NewRedisContainerConnector(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	topic := "latest-" + testkit.NewID()
	queue, err := New(&Config{Driver: DriverRedisStream}, WithRedisConnector(conn))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, queue.Close()) })
	require.NoError(t, queue.Publish(ctx, topic, []byte("retained")))
	hook := &gapPublishHook{client: conn.GetClient(), topic: topic}
	conn.GetClient().AddHook(hook)
	received := make(chan string, 4)
	sub, err := queue.Subscribe(ctx, topic, func(msg Message) error { received <- string(msg.Data()); return nil }, FromLatest(), WithAutoAck())
	require.NoError(t, err)
	defer sub.Unsubscribe()
	select {
	case value := <-received:
		require.Equal(t, "gap", value)
	case <-ctx.Done():
		t.Fatal("Message published between empty reads was skipped")
	}
	require.True(t, hook.inserted.Load())
	require.NoError(t, queue.Publish(ctx, topic, []byte("next")))
	select {
	case value := <-received:
		require.Equal(t, "next", value)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestRedisBroadcastLatestOnMissingStream(t *testing.T) {
	t.Parallel()
	conn := testkit.NewRedisContainerConnector(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queue, err := New(&Config{Driver: DriverRedisStream}, WithRedisConnector(conn))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, queue.Close()) })
	topic := testkit.NewID()
	received := make(chan struct{}, 1)
	sub, err := queue.Subscribe(ctx, topic, func(Message) error { received <- struct{}{}; return nil }, FromLatest(), WithAutoAck())
	require.NoError(t, err)
	defer sub.Unsubscribe()
	require.NoError(t, queue.Publish(ctx, topic, []byte("first")))
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
