package mq

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
)

type blockingDrainSubscription struct {
	started   chan struct{}
	done      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	forced    atomic.Bool
}

func (s *blockingDrainSubscription) Drain(ctx context.Context) error {
	s.startOnce.Do(func() { close(s.started) })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *blockingDrainSubscription) Unsubscribe() error {
	s.forced.Store(true)
	s.stopOnce.Do(func() { close(s.done) })
	return nil
}

func (s *blockingDrainSubscription) Done() <-chan struct{} { return s.done }

func TestDrainTimeoutDoesNotPreventForceClose(t *testing.T) {
	t.Parallel()
	q := newMQ(&mockTransport{}, clog.Discard(), metrics.Discard()).(*mq)
	sub := &blockingDrainSubscription{started: make(chan struct{}), done: make(chan struct{})}
	q.subscriptions[sub] = struct{}{}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, q.Drain(ctx), context.DeadlineExceeded)
	require.False(t, sub.forced.Load())
	require.NoError(t, q.Close())
	require.True(t, sub.forced.Load())
	require.NoError(t, q.Close())
}

func TestDrainCallerDeadlineDoesNotCancelSharedCleanup(t *testing.T) {
	t.Parallel()
	q := newMQ(&mockTransport{}, clog.Discard(), metrics.Discard()).(*mq)
	sub := &blockingDrainSubscription{started: make(chan struct{}), done: make(chan struct{})}
	q.subscriptions[sub] = struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { first <- q.Drain(ctx) }()
	<-sub.started
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	second := make(chan error, 1)
	go func() { second <- q.Drain(t.Context()) }()
	require.False(t, sub.forced.Load())
	sub.stopOnce.Do(func() { close(sub.done) })
	require.NoError(t, <-second)
}

type instrumentCountingMeter struct {
	metrics.Meter
	calls atomic.Int32
}

func (m *instrumentCountingMeter) Counter(name, desc string, opts ...metrics.MetricOption) (metrics.Counter, error) {
	m.calls.Add(1)
	return m.Meter.Counter(name, desc, opts...)
}

func (m *instrumentCountingMeter) Histogram(name, desc string, opts ...metrics.MetricOption) (metrics.Histogram, error) {
	m.calls.Add(1)
	return m.Meter.Histogram(name, desc, opts...)
}

func TestMessageHotPathReusesInstruments(t *testing.T) {
	t.Parallel()
	meter := &instrumentCountingMeter{Meter: metrics.Discard()}
	q := newMQ(&mockTransport{}, clog.Discard(), meter).(*mq)
	require.EqualValues(t, 4, meter.calls.Load())
	for range 100 {
		require.NoError(t, q.Publish(t.Context(), "topic", nil))
		require.NoError(t, q.wrapHandler("topic", func(Message) error { return nil }, subscribeOptions{})(&mockMessage{}))
	}
	require.EqualValues(t, 4, meter.calls.Load())
	require.NoError(t, q.Close())
}
