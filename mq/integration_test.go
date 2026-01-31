package mq

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ceyewan/genesis/testkit"
	"github.com/stretchr/testify/require"
)

func newJetStreamMQ(t *testing.T) MQ {
	t.Helper()

	kit := testkit.NewKit(t)
	natsConn := testkit.NewNATSContainerConnector(t)

	cfg := &Config{
		Driver: DriverNATSJetStream,
		JetStream: &JetStreamConfig{
			AutoCreateStream: true,
		},
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
	})
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
	})
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

	for i := 0; i < 3; i++ {
		sub, err := mq.Subscribe(ctx, subject, func(msg Message) error {
			wg.Done()
			return nil
		}, WithQueueGroup(group))
		require.NoError(t, err)
		t.Cleanup(func() { _ = sub.Unsubscribe() })
	}

	time.Sleep(100 * time.Millisecond)
	for i := 0; i < messageCount; i++ {
		require.NoError(t, mq.Publish(ctx, subject, []byte(fmt.Sprintf("msg-%d", i))))
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
	}, WithQueueGroup(groupA))
	require.NoError(t, err)
	t.Cleanup(func() { _ = subA.Unsubscribe() })

	subB, err := mq.Subscribe(ctx, subject, func(msg Message) error {
		wgB.Done()
		return nil
	}, WithQueueGroup(groupB))
	require.NoError(t, err)
	t.Cleanup(func() { _ = subB.Unsubscribe() })

	time.Sleep(100 * time.Millisecond)
	for i := 0; i < messageCount; i++ {
		require.NoError(t, mq.Publish(ctx, subject, []byte(fmt.Sprintf("msg-%d", i))))
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
	}, WithDurable(durable))
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
	}, WithDurable(durable))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub2.Unsubscribe() })

	waitTimeout(t, second, 5*time.Second)
}
