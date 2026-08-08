package mq

import (
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/testkit"
)

func newJetStreamMQ(t *testing.T) MQ {
	return newJetStreamMQWithConfig(t, &JetStreamConfig{AutoCreateStream: true})
}

func newJetStreamMQWithConfig(t *testing.T, jsCfg *JetStreamConfig) MQ {
	t.Helper()

	kit := testkit.NewKit(t)
	natsConn := testkit.NewNATSContainerConnector(t)

	cfg := &Config{
		Driver:    DriverNATSJetStream,
		JetStream: jsCfg,
	}

	mq, err := New(cfg,
		WithNATSConnector(natsConn),
		WithLogger(kit.Logger),
		WithMeter(kit.Meter),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mq.Close() })

	return mq
}

func uniqueSubject() string {
	return fmt.Sprintf("t%s.event", testkit.NewID())
}

func uniqueGroup() string {
	return fmt.Sprintf("g%s", testkit.NewID())
}

func waitTimeout(t *testing.T, done <-chan struct{}, timeout time.Duration) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("timeout waiting for message")
	}
}

func TestJetStreamPublishSubscribeIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 5*time.Second)
	defer cancel()

	mq := newJetStreamMQ(t)
	subject := uniqueSubject()

	done := make(chan struct{})
	sub, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		if string(msg.Data()) != "hello" {
			t.Fatalf("unexpected payload: %s", string(msg.Data()))
		}
		close(done)
		return nil
	}, WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, mq.Publish(ctx, subject, []byte("hello")))

	waitTimeout(t, done, 3*time.Second)
}

func TestJetStreamHeadersIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 5*time.Second)
	defer cancel()

	mq := newJetStreamMQ(t)
	subject := uniqueSubject()

	done := make(chan struct{})
	sub, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		if msg.Headers().Get("trace-id") != "abc123" {
			t.Fatalf("unexpected trace-id: %s", msg.Headers().Get("trace-id"))
		}
		close(done)
		return nil
	}, WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, mq.Publish(ctx, subject, []byte("payload"), WithHeader("trace-id", "abc123")))

	waitTimeout(t, done, 3*time.Second)
}

func TestJetStreamQueueGroupIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	mq := newJetStreamMQ(t)
	subject := uniqueSubject()
	group := uniqueGroup()

	const messageCount = 10
	var wg sync.WaitGroup
	wg.Add(messageCount)

	for range 3 {
		sub, err := mq.Subscribe(ctx, subject, func(msg Message) error {
			wg.Done()
			return nil
		}, WithQueueGroup(group), WithAutoAck())
		require.NoError(t, err)
		t.Cleanup(func() { _ = sub.Unsubscribe() })
	}

	time.Sleep(100 * time.Millisecond)
	for i := range messageCount {
		require.NoError(t, mq.Publish(ctx, subject, fmt.Appendf(nil, "msg-%d", i)))
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	waitTimeout(t, done, 5*time.Second)
}

func TestJetStreamMultiGroupBroadcastIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	mq := newJetStreamMQ(t)
	subject := uniqueSubject()
	groupA := uniqueGroup()
	groupB := uniqueGroup()

	const messageCount = 5
	var wgA sync.WaitGroup
	var wgB sync.WaitGroup
	wgA.Add(messageCount)
	wgB.Add(messageCount)

	subA, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		wgA.Done()
		return nil
	}, WithQueueGroup(groupA), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = subA.Unsubscribe() })

	subB, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		wgB.Done()
		return nil
	}, WithQueueGroup(groupB), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = subB.Unsubscribe() })

	time.Sleep(100 * time.Millisecond)
	for i := range messageCount {
		require.NoError(t, mq.Publish(ctx, subject, fmt.Appendf(nil, "msg-%d", i)))
	}

	doneA := make(chan struct{})
	doneB := make(chan struct{})
	go func() {
		wgA.Wait()
		close(doneA)
	}()
	go func() {
		wgB.Wait()
		close(doneB)
	}()

	waitTimeout(t, doneA, 5*time.Second)
	waitTimeout(t, doneB, 5*time.Second)
}

func TestJetStreamDurableResumeIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	mq := newJetStreamMQ(t)
	subject := uniqueSubject()
	durable := "d-" + testkit.NewID()

	first := make(chan struct{})
	sub, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		if string(msg.Data()) == "first" {
			close(first)
		}
		return nil
	}, WithDurable(durable), WithAutoAck())
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.NoError(t, mq.Publish(ctx, subject, []byte("first")))
	waitTimeout(t, first, 5*time.Second)

	require.NoError(t, sub.Unsubscribe())
	waitTimeout(t, sub.Done(), 5*time.Second)

	require.NoError(t, mq.Publish(ctx, subject, []byte("second")))

	second := make(chan struct{})
	sub2, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		if string(msg.Data()) == "second" {
			close(second)
		}
		return nil
	}, WithDurable(durable), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub2.Unsubscribe() })

	waitTimeout(t, second, 5*time.Second)
}

func TestJetStreamManualNakWithDelayRedeliveryIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	q := newJetStreamMQ(t)
	subject := uniqueSubject()
	var deliveries atomic.Int32
	done := make(chan struct{})
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		switch deliveries.Add(1) {
		case 1:
			return msg.NakWithDelay(50 * time.Millisecond)
		case 2:
			if err := msg.Ack(); err != nil {
				return err
			}
			close(done)
		}
		return nil
	}, WithDurable("d-"+testkit.NewID()), WithManualAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	require.NoError(t, q.Publish(ctx, subject, []byte("retry")))
	waitTimeout(t, done, 5*time.Second)
	require.Equal(t, int32(2), deliveries.Load())
}

func TestJetStreamAutoCreateConfigAndPublishAckIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	q := newJetStreamMQWithConfig(t, &JetStreamConfig{
		AutoCreateStream: true,
		AckWait:          2 * time.Second,
		MaxDeliver:       7,
		Retention:        StreamRetentionLimits,
		Storage:          StreamStorageMemory,
		MaxAge:           time.Hour,
		MaxBytes:         1 << 20,
		Replicas:         1,
	})
	subject := uniqueSubject()
	durable := "d-" + testkit.NewID()
	done := make(chan struct{})
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		close(done)
		return nil
	}, WithDurable(durable), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	transport := q.(*mq).transport.(*natsJetStreamTransport)
	stream, err := transport.js.Stream(ctx, transport.getStreamName(subject))
	require.NoError(t, err)
	streamInfo, err := stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, jetstream.LimitsPolicy, streamInfo.Config.Retention)
	require.Equal(t, jetstream.MemoryStorage, streamInfo.Config.Storage)
	require.Equal(t, time.Hour, streamInfo.Config.MaxAge)
	require.Equal(t, int64(1<<20), streamInfo.Config.MaxBytes)
	require.Equal(t, 1, streamInfo.Config.Replicas)

	consumer, err := stream.Consumer(ctx, durable)
	require.NoError(t, err)
	consumerInfo, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 7, consumerInfo.Config.MaxDeliver)
	require.Equal(t, 2*time.Second, consumerInfo.Config.AckWait)

	// Publish 返回 nil 代表 JetStream 已返回 PubAck，而不只是写入本地 socket。
	require.NoError(t, q.Publish(ctx, subject, []byte("acked-by-broker")))
	waitTimeout(t, done, 5*time.Second)
	streamInfo, err = stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), streamInfo.State.Msgs)
}

func TestJetStreamMaxDeliverIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	q := newJetStreamMQWithConfig(t, &JetStreamConfig{
		AutoCreateStream: true,
		AckWait:          100 * time.Millisecond,
		MaxDeliver:       3,
	})
	subject := uniqueSubject()
	var deliveries atomic.Int32
	third := make(chan struct{})
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		if deliveries.Add(1) == 3 {
			close(third)
		}
		return msg.Nak()
	}, WithDurable("d-"+testkit.NewID()), WithManualAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	require.NoError(t, q.Publish(ctx, subject, []byte("poison")))
	waitTimeout(t, third, 5*time.Second)
	time.Sleep(250 * time.Millisecond)
	require.Equal(t, int32(3), deliveries.Load())
}

func TestJetStreamMaxInflightBackpressureIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	q := newJetStreamMQ(t)
	subject := uniqueSubject()
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var deliveries atomic.Int32
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		if deliveries.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
		} else {
			close(secondStarted)
		}
		return msg.Ack()
	}, WithDurable("d-"+testkit.NewID()), WithMaxInflight(1), WithManualAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	require.NoError(t, q.Publish(ctx, subject, []byte("first")))
	require.NoError(t, q.Publish(ctx, subject, []byte("second")))
	waitTimeout(t, firstStarted, 5*time.Second)
	select {
	case <-secondStarted:
		t.Fatal("second message bypassed MaxInflight=1 backpressure")
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
	waitTimeout(t, secondStarted, 5*time.Second)
}

func TestJetStreamDrainWaitsForHandlerIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	q := newJetStreamMQ(t)
	subject := uniqueSubject()
	started := make(chan struct{})
	release := make(chan struct{})
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		close(started)
		<-release
		return msg.Ack()
	}, WithDurable("d-"+testkit.NewID()), WithManualAck())
	require.NoError(t, err)

	require.NoError(t, q.Publish(ctx, subject, []byte("drain")))
	waitTimeout(t, started, 5*time.Second)

	drainCtx, drainCancel := testkit.NewContext(t, 5*time.Second)
	defer drainCancel()
	drained := make(chan error, 1)
	go func() { drained <- q.Drain(drainCtx) }()
	select {
	case err := <-drained:
		t.Fatalf("Drain returned before active handler completed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-drained)
	waitTimeout(t, sub.Done(), 5*time.Second)
	require.ErrorIs(t, q.Publish(ctx, subject, []byte("closed")), ErrClosed)
}

func TestJetStreamReconnectAndResumeIntegration(t *testing.T) {
	ctx, cancel := testkit.NewContext(t, 20*time.Second)
	defer cancel()

	container, natsCfg := testkit.NewNATSContainer(t)
	natsCfg.MaxReconnects = 100
	natsCfg.ReconnectWait = 50 * time.Millisecond
	natsCfg.PingInterval = 100 * time.Millisecond
	conn, err := connector.NewNATS(natsCfg, connector.WithLogger(testkit.NewLogger()))
	require.NoError(t, err)
	require.NoError(t, conn.Connect(ctx))
	t.Cleanup(func() { _ = conn.Close() })

	q, err := New(&Config{
		Driver: DriverNATSJetStream,
		JetStream: &JetStreamConfig{
			AutoCreateStream: true,
			AckWait:          time.Second,
			MaxDeliver:       5,
		},
	}, WithNATSConnector(conn), WithLogger(testkit.NewLogger()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = q.Close() })

	subject := uniqueSubject()
	durable := "d-" + testkit.NewID()
	before := make(chan struct{})
	after := make(chan struct{})
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		switch string(msg.Data()) {
		case "before":
			close(before)
		case "after":
			close(after)
		}
		return nil
	}, WithDurable(durable), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	require.NoError(t, q.Publish(ctx, subject, []byte("before")))
	waitTimeout(t, before, 5*time.Second)

	containerID := container.GetContainerID()
	pauseOutput, pauseErr := exec.CommandContext(ctx, "docker", "pause", containerID).CombinedOutput()
	require.NoErrorf(t, pauseErr, "docker pause: %s", pauseOutput)
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", containerID).Run() })
	require.Eventually(t, func() bool {
		return conn.GetClient().Status() != natsgo.CONNECTED
	}, 5*time.Second, 20*time.Millisecond)
	require.False(t, conn.IsHealthy())
	unpauseOutput, unpauseErr := exec.CommandContext(ctx, "docker", "unpause", containerID).CombinedOutput()
	require.NoErrorf(t, unpauseErr, "docker unpause: %s", unpauseOutput)
	reconnectDeadline := time.Now().Add(10 * time.Second)
	for conn.GetClient().Status() != natsgo.CONNECTED && time.Now().Before(reconnectDeadline) {
		time.Sleep(25 * time.Millisecond)
	}
	require.Equalf(t, natsgo.CONNECTED, conn.GetClient().Status(), "last error: %v", conn.GetClient().LastError())
	require.True(t, conn.IsHealthy())

	require.NoError(t, q.Publish(ctx, subject, []byte("after")))
	waitTimeout(t, after, 5*time.Second)
}
