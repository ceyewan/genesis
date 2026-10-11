package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/idem"
	"github.com/ceyewan/genesis/internal/testkit"
	"github.com/ceyewan/genesis/mq"
)

func TestDurableTaskDeduplicationAndWorkerRestart(t *testing.T) {
	t.Parallel()
	nats := testkit.NewNATSContainerConnector(t)
	redis := testkit.NewRedisContainerConnector(t)
	queue, err := mq.New(&mq.Config{Driver: mq.DriverNATSJetStream, JetStream: &mq.JetStreamConfig{AutoCreateStream: true, AckWait: time.Second, MaxDeliver: 5}}, mq.WithNATSConnector(nats))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, queue.Close()) })
	cfg := &idem.Config{Driver: idem.DriverRedis, Prefix: "tasks:" + testkit.NewID() + ":"}
	ctx, cancel := testkit.NewContext(t, 15*time.Second)
	defer cancel()
	topic := "tasks." + testkit.NewID()
	group := "workers-" + testkit.NewID()
	first, err := idem.New(cfg, idem.WithRedisConnector(redis))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	require.NoError(t, verifyPhase(ctx, queue, first, topic, group, true, 1))
	require.NoError(t, first.Close())
	// 新建 Worker 和 Idempotency；去重记录必须由 Redis 保留，而不是内存状态。
	restarted, err := idem.New(cfg, idem.WithRedisConnector(redis))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	require.NoError(t, verifyPhase(ctx, queue, restarted, topic, group, false, 0))
}
