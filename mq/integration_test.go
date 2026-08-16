package mq

import (
	"context"
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
	"github.com/ceyewan/genesis/internal/testkit"
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

func newRedisStreamMQ(t *testing.T) MQ {
	t.Helper()
	kit := testkit.NewKit(t)
	redisConn := testkit.NewRedisContainerConnector(t)
	q, err := New(&Config{
		Driver:      DriverRedisStream,
		RedisStream: &RedisStreamConfig{},
	}, WithRedisConnector(redisConn), WithLogger(kit.Logger), WithMeter(kit.Meter))
	require.NoError(t, err)
	t.Cleanup(func() { _ = q.Close() })
	return q
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
	mq := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 5*time.Second)
	defer cancel()

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
	mq := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 5*time.Second)
	defer cancel()

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

func TestJetStreamProgressMessageIntegration(t *testing.T) {
	const ackWait = 500 * time.Millisecond
	q := newJetStreamMQWithConfig(t, &JetStreamConfig{
		AutoCreateStream: true,
		AckWait:          ackWait,
	})
	ctx, cancel := testkit.NewContext(t, 6*time.Second)
	defer cancel()
	subject := uniqueSubject()

	var deliveries atomic.Int32
	handlerResults := make(chan error, 4)
	sub, err := q.Subscribe(ctx, subject, func(msg Message) error {
		if delivery := deliveries.Add(1); delivery != 1 {
			handlerResults <- fmt.Errorf("message redelivered while first delivery was still active: %d", delivery)
			return nil
		}
		progress, ok := msg.(ProgressMessage)
		if !ok {
			err := fmt.Errorf("JetStream message does not implement ProgressMessage")
			handlerResults <- err
			return err
		}

		work := time.NewTimer(1200 * time.Millisecond)
		defer work.Stop()
		heartbeat := time.NewTicker(100 * time.Millisecond)
		defer heartbeat.Stop()
		for {
			select {
			case <-heartbeat.C:
				if progressErr := progress.InProgress(); progressErr != nil {
					handlerResults <- progressErr
					return progressErr
				}
			case <-work.C:
				handlerResults <- nil
				return nil
			case <-ctx.Done():
				handlerResults <- ctx.Err()
				return ctx.Err()
			}
		}
	}, WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })

	require.NoError(t, q.Publish(ctx, subject, []byte("progress")))
	select {
	case handlerErr := <-handlerResults:
		require.NoError(t, handlerErr)
	case <-time.After(4 * time.Second):
		t.Fatal("timeout waiting for long-running handler")
	}

	observation := time.NewTimer(2 * ackWait)
	defer observation.Stop()
	select {
	case handlerErr := <-handlerResults:
		t.Fatalf("unexpected additional delivery: %v", handlerErr)
	case <-observation.C:
	}
	require.Equal(t, int32(1), deliveries.Load())
}

func TestJetStreamQueueGroupIntegration(t *testing.T) {
	mq := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

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

func TestJetStreamConsumerIdentityIsScopedByTopicIntegration(t *testing.T) {
	q := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 15*time.Second)
	defer cancel()

	tests := []struct {
		name   string
		option func(string) SubscribeOption
	}{
		{name: "queue group", option: WithQueueGroup},
		{name: "durable", option: WithDurable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := "t" + testkit.NewID()
			firstTopic := base + ".first"
			secondTopic := base + ".second"
			identity := "consumer-" + testkit.NewID()
			firstDone := make(chan string, 1)
			secondDone := make(chan string, 1)

			first, err := q.Subscribe(ctx, firstTopic, func(msg Message) error {
				firstDone <- msg.Topic()
				return nil
			}, tt.option(identity), WithAutoAck())
			require.NoError(t, err)
			t.Cleanup(func() { _ = first.Unsubscribe() })

			second, err := q.Subscribe(ctx, secondTopic, func(msg Message) error {
				secondDone <- msg.Topic()
				return nil
			}, tt.option(identity), WithAutoAck())
			require.NoError(t, err)
			t.Cleanup(func() { _ = second.Unsubscribe() })

			require.NoError(t, q.Publish(ctx, firstTopic, []byte("first")))
			require.NoError(t, q.Publish(ctx, secondTopic, []byte("second")))
			select {
			case got := <-firstDone:
				require.Equal(t, firstTopic, got)
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for first topic")
			}
			select {
			case got := <-secondDone:
				require.Equal(t, secondTopic, got)
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for second topic")
			}
		})
	}
}

func TestJetStreamMultiGroupBroadcastIntegration(t *testing.T) {
	mq := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

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
	mq := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

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
	q := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

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
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
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

	consumer, err := stream.Consumer(ctx, durableConsumerName(durable, subject))
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

func TestJetStreamAutoCreateOnPublishIntegration(t *testing.T) {
	q := newJetStreamMQWithConfig(t, &JetStreamConfig{
		AutoCreateStream: true,
		Storage:          StreamStorageMemory,
	})
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
	subject := uniqueSubject()

	// 没有消费者先行创建 Stream 时，生产者也应能独立发布。
	require.NoError(t, q.Publish(ctx, subject, []byte("publisher-created-stream")))

	transport := q.(*mq).transport.(*natsJetStreamTransport)
	stream, err := transport.js.Stream(ctx, transport.getStreamName(subject))
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.True(t, streamSubjectsCoverTopic(info.Config.Subjects, subject))
	require.Equal(t, uint64(1), info.State.Msgs)
}

func TestJetStreamAutoCreateConcurrentDomainTopicsIntegration(t *testing.T) {
	kit := testkit.NewKit(t)
	natsConn := testkit.NewNATSContainerConnector(t)
	cfg := &Config{
		Driver: DriverNATSJetStream,
		JetStream: &JetStreamConfig{
			AutoCreateStream: true,
			Storage:          StreamStorageMemory,
		},
	}
	newClient := func() MQ {
		client, err := New(cfg,
			WithNATSConnector(natsConn),
			WithLogger(kit.Logger),
			WithMeter(kit.Meter),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	firstClient := newClient()
	secondClient := newClient()

	ctx, cancel := testkit.NewContext(t, 15*time.Second)
	defer cancel()
	root := "t" + testkit.NewID()
	transport := firstClient.(*mq).transport.(*natsJetStreamTransport)
	_, err := transport.js.CreateStream(ctx, jetstream.StreamConfig{
		Name:     transport.getStreamName(root + ".seed"),
		Subjects: []string{root + ".seed"},
		Storage:  jetstream.MemoryStorage,
		Replicas: 1,
	})
	require.NoError(t, err)

	const topicCount = 16
	topics := make([]string, topicCount)
	start := make(chan struct{})
	errorsByTopic := make(chan error, topicCount)
	var publishers sync.WaitGroup
	for i := range topicCount {
		topics[i] = fmt.Sprintf("%s.event-%d", root, i)
		client := firstClient
		if i%2 == 1 {
			client = secondClient
		}
		topic := topics[i]
		publishers.Go(func() {
			<-start
			errorsByTopic <- client.Publish(ctx, topic, []byte(topic))
		})
	}
	close(start)
	publishers.Wait()
	close(errorsByTopic)
	for publishErr := range errorsByTopic {
		require.NoError(t, publishErr)
	}

	stream, err := transport.js.Stream(ctx, transport.getStreamName(topics[0]))
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{root, root + ".>"}, info.Config.Subjects)
	for _, topic := range topics {
		require.Truef(t, streamSubjectsCoverTopic(info.Config.Subjects, topic),
			"stream subjects %v do not cover %s", info.Config.Subjects, topic)
	}
}

func TestJetStreamMaxDeliverIntegration(t *testing.T) {
	q := newJetStreamMQWithConfig(t, &JetStreamConfig{
		AutoCreateStream: true,
		AckWait:          100 * time.Millisecond,
		MaxDeliver:       3,
	})
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
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
	q := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

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
	q := newJetStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

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

func TestRedisStreamDefaultStartConsumesRetainedMessages(t *testing.T) {
	q := newRedisStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
	topic := uniqueSubject()
	require.NoError(t, q.Publish(ctx, topic, []byte("before-subscribe")))
	done := make(chan struct{})
	sub, err := q.Subscribe(ctx, topic, func(msg Message) error {
		require.Equal(t, "before-subscribe", string(msg.Data()))
		close(done)
		return nil
	}, WithQueueGroup("g-"+testkit.NewID()), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	waitTimeout(t, done, 5*time.Second)
}

func TestRedisStreamFromLatestSkipsRetainedMessages(t *testing.T) {
	q := newRedisStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()
	topic := uniqueSubject()
	require.NoError(t, q.Publish(ctx, topic, []byte("old")))
	done := make(chan string, 1)
	sub, err := q.Subscribe(ctx, topic, func(msg Message) error {
		done <- string(msg.Data())
		return nil
	}, WithQueueGroup("g-"+testkit.NewID()), FromLatest(), WithAutoAck())
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	require.NoError(t, q.Publish(ctx, topic, []byte("new")))
	select {
	case got := <-done:
		require.Equal(t, "new", got)
	case <-time.After(5 * time.Second):
		t.Fatal("latest subscription did not receive new message")
	}
}

func TestRedisStreamDrainPreservesActiveHandlerContext(t *testing.T) {
	q := newRedisStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	topic := uniqueSubject()
	handlerContext := make(chan context.Context, 1)
	release := make(chan struct{})
	sub, err := q.Subscribe(ctx, topic, func(msg Message) error {
		handlerContext <- msg.Context()
		<-release
		return msg.Ack()
	}, WithQueueGroup(uniqueGroup()), WithDurable("d-"+testkit.NewID()), WithManualAck())
	require.NoError(t, err)

	require.NoError(t, q.Publish(ctx, topic, []byte("drain")))
	messageCtx := <-handlerContext
	drained := make(chan error, 1)
	go func() { drained <- sub.Drain(ctx) }()
	select {
	case err := <-drained:
		t.Fatalf("Drain returned before active handler completed: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, messageCtx.Err(), "graceful Drain canceled the active handler context")
	close(release)
	require.NoError(t, <-drained)
}

func TestRedisStreamDrainDeadlineCancelsActiveHandler(t *testing.T) {
	q := newRedisStreamMQ(t)
	ctx, cancel := testkit.NewContext(t, 10*time.Second)
	defer cancel()

	topic := uniqueSubject()
	handlerStarted := make(chan struct{})
	handlerDone := make(chan error, 1)
	sub, err := q.Subscribe(ctx, topic, func(msg Message) error {
		close(handlerStarted)
		<-msg.Context().Done()
		handlerDone <- msg.Context().Err()
		return msg.Context().Err()
	}, WithQueueGroup(uniqueGroup()), WithDurable("d-"+testkit.NewID()), WithManualAck())
	require.NoError(t, err)

	require.NoError(t, q.Publish(ctx, topic, []byte("force")))
	waitTimeout(t, handlerStarted, 5*time.Second)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer drainCancel()
	require.ErrorIs(t, sub.Drain(drainCtx), context.DeadlineExceeded)
	require.ErrorIs(t, <-handlerDone, context.Canceled)
	waitTimeout(t, sub.Done(), 5*time.Second)
}

func TestJetStreamReconnectAndResumeIntegration(t *testing.T) {
	container, natsCfg := testkit.NewNATSContainer(t)
	ctx, cancel := testkit.NewContext(t, 20*time.Second)
	defer cancel()

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
	require.Eventually(t, func() bool {
		return !conn.IsHealthy()
	}, 5*time.Second, 20*time.Millisecond)
	unpauseOutput, unpauseErr := exec.CommandContext(ctx, "docker", "unpause", containerID).CombinedOutput()
	require.NoErrorf(t, unpauseErr, "docker unpause: %s", unpauseOutput)
	require.Eventuallyf(t, func() bool {
		return conn.GetClient().Status() == natsgo.CONNECTED && conn.IsHealthy()
	}, 10*time.Second, 20*time.Millisecond, "last error: %v", conn.GetClient().LastError())

	require.NoError(t, q.Publish(ctx, subject, []byte("after")))
	waitTimeout(t, after, 5*time.Second)
}
