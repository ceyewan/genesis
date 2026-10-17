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
	"github.com/ceyewan/genesis/xerrors"
)

type initializingSubscription struct {
	cancel         context.CancelFunc
	done           chan struct{}
	draining       chan struct{}
	drainOnce      sync.Once
	forced         atomic.Bool
	unsubscribeErr error
}

func (s *initializingSubscription) Unsubscribe() error {
	s.forced.Store(true)
	s.cancel()
	return s.unsubscribeErr
}

func (s *initializingSubscription) Drain(ctx context.Context) error {
	s.drainOnce.Do(func() { close(s.draining) })
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *initializingSubscription) Done() <-chan struct{} { return s.done }

type initializationMessage struct {
	mockMessage
	ctx context.Context
}

func (m *initializationMessage) Context() context.Context { return m.ctx }

type initializingTransport struct {
	mockTransport
	sub                        *initializingSubscription
	started                    chan struct{}
	returnSubscription         chan struct{}
	handlerStarted             chan struct{}
	finishHandler              chan struct{}
	ignoreInitializationCancel bool
	unsubscribeErr             error
}

func newInitializingTransport() *initializingTransport {
	return &initializingTransport{
		started: make(chan struct{}), returnSubscription: make(chan struct{}),
		handlerStarted: make(chan struct{}), finishHandler: make(chan struct{}),
	}
}

func (tr *initializingTransport) Subscribe(ctx context.Context, _ string, handler Handler, _ subscribeOptions) (Subscription, error) {
	ctx, cancel := context.WithCancel(ctx)
	tr.sub = &initializingSubscription{cancel: cancel, done: make(chan struct{}), draining: make(chan struct{}), unsubscribeErr: tr.unsubscribeErr}
	go func() {
		defer close(tr.sub.done)
		_ = handler(&initializationMessage{ctx: ctx})
	}()
	close(tr.started)
	if tr.ignoreInitializationCancel {
		<-tr.returnSubscription
	} else {
		select {
		case <-tr.returnSubscription:
		case <-ctx.Done():
		}
	}
	return tr.sub, nil
}

type concurrentInitializingTransport struct {
	mockTransport
	release chan struct{}
}

func (tr *concurrentInitializingTransport) Subscribe(ctx context.Context, _ string, handler Handler, _ subscribeOptions) (Subscription, error) {
	ctx, cancel := context.WithCancel(ctx)
	sub := &initializingSubscription{cancel: cancel, done: make(chan struct{}), draining: make(chan struct{})}
	go func() { defer close(sub.done); _ = handler(&initializationMessage{ctx: ctx}) }()
	select {
	case <-ctx.Done():
	case <-tr.release:
	}
	return sub, nil
}

func TestCloseTracksAllConcurrentInitializers(t *testing.T) {
	t.Parallel()
	const count = 32
	tr := &concurrentInitializingTransport{release: make(chan struct{})}
	q := newMQ(tr, clog.Discard(), metrics.Discard())
	started := make(chan struct{}, count)
	errs := make(chan error, count)
	var active atomic.Int32
	for range count {
		go func() {
			_, err := q.Subscribe(t.Context(), "topic", func(msg Message) error {
				active.Add(1)
				defer active.Add(-1)
				started <- struct{}{}
				<-msg.Context().Done()
				return nil
			})
			errs <- err
		}()
	}
	for range count {
		<-started
	}
	closeErr := q.Close()
	activeOnReturn := active.Load()
	close(tr.release)
	for range count {
		require.ErrorIs(t, <-errs, ErrClosed)
	}
	require.NoError(t, closeErr)
	require.Zero(t, activeOnReturn, "Close missed active handlers in concurrent initializers")
}

func TestCloseWaitsForInitializingSubscription(t *testing.T) {
	t.Parallel()
	tr := newInitializingTransport()
	q := newMQ(tr, clog.Discard(), metrics.Discard())
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	subscribed := make(chan error, 1)
	go func() {
		_, err := q.Subscribe(ctx, "topic", func(msg Message) error {
			close(tr.handlerStarted)
			<-msg.Context().Done()
			return nil
		})
		subscribed <- err
	}()
	<-tr.started
	<-tr.handlerStarted
	closeErr := q.Close()
	forcedOnReturn := tr.sub.forced.Load()
	var doneOnReturn bool
	select {
	case <-tr.sub.Done():
		doneOnReturn = true
	default:
	}
	// Release even on the buggy implementation so the regression fails cleanly.
	close(tr.returnSubscription)
	require.ErrorIs(t, <-subscribed, ErrClosed)
	<-tr.sub.Done()
	require.NoError(t, closeErr)
	require.True(t, forcedOnReturn, "Close missed a subscription whose handler already started")
	require.True(t, doneOnReturn, "Close returned success before the handler stopped")
}

func TestClosePreservesLateSubscriptionCleanupError(t *testing.T) {
	t.Parallel()
	tr := newInitializingTransport()
	tr.unsubscribeErr = xerrors.New("late unsubscribe failed")
	q := newMQ(tr, clog.Discard(), metrics.Discard())
	subscribed := make(chan error, 1)
	go func() {
		_, err := q.Subscribe(t.Context(), "topic", func(msg Message) error {
			close(tr.handlerStarted)
			<-msg.Context().Done()
			return nil
		})
		subscribed <- err
	}()
	<-tr.started
	<-tr.handlerStarted
	require.ErrorIs(t, q.Close(), tr.unsubscribeErr)
	err := <-subscribed
	require.ErrorIs(t, err, ErrClosed)
	require.ErrorIs(t, err, tr.unsubscribeErr)
}

func TestCloseCannotSucceedBeforeUncooperativeInitializerReturns(t *testing.T) {
	t.Parallel()
	tr := newInitializingTransport()
	tr.ignoreInitializationCancel = true
	q := newMQ(tr, clog.Discard(), metrics.Discard())
	subscribed := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		_, err := q.Subscribe(t.Context(), "topic", func(msg Message) error {
			close(tr.handlerStarted)
			<-msg.Context().Done()
			close(stopped)
			return nil
		})
		subscribed <- err
	}()
	<-tr.started
	<-tr.handlerStarted
	closed := make(chan error, 1)
	go func() { closed <- q.Close() }()
	<-stopped
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	drainErr := q.Drain(ctx)
	var returned bool
	select {
	case <-closed:
		returned = true
	default:
	}
	close(tr.returnSubscription)
	require.ErrorIs(t, <-subscribed, ErrClosed)
	if !returned {
		require.NoError(t, <-closed)
	}
	require.False(t, returned, "shutdown completed while transport setup was still running")
	require.ErrorIs(t, drainErr, context.DeadlineExceeded)
}

func TestDrainStartsReadySubscriptionsBeforeWaitingForInitialization(t *testing.T) {
	t.Parallel()
	tr := newInitializingTransport()
	q := newMQ(tr, clog.Discard(), metrics.Discard()).(*mq)
	ready := &blockingDrainSubscription{started: make(chan struct{}), done: make(chan struct{})}
	q.subscriptions[ready] = struct{}{}
	subscribed := make(chan error, 1)
	go func() {
		_, err := q.Subscribe(t.Context(), "initializing", func(msg Message) error {
			close(tr.handlerStarted)
			<-msg.Context().Done()
			return nil
		})
		subscribed <- err
	}()
	<-tr.started
	<-tr.handlerStarted
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	drained := make(chan error, 1)
	go func() { drained <- q.Drain(ctx) }()
	var started bool
	select {
	case <-ready.started:
		started = true
	case <-time.After(time.Second):
	}
	cancel()
	drainErr := <-drained
	closeErr := q.Close()
	subscribeErr := <-subscribed
	require.True(t, started, "a pending initializer prevented an existing subscription from stopping intake")
	require.ErrorIs(t, drainErr, context.Canceled)
	require.NoError(t, closeErr)
	require.ErrorIs(t, subscribeErr, ErrClosed)
}

func TestDrainIncludesLateSubscriptionAndCloseCanEscalate(t *testing.T) {
	t.Parallel()
	tr := newInitializingTransport()
	q := newMQ(tr, clog.Discard(), metrics.Discard())
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	subscribed := make(chan error, 1)
	var handlerContext context.Context
	go func() {
		_, err := q.Subscribe(ctx, "topic", func(msg Message) error {
			handlerContext = msg.Context()
			close(tr.handlerStarted)
			select {
			case <-msg.Context().Done():
			case <-tr.finishHandler:
			}
			return nil
		})
		subscribed <- err
	}()
	<-tr.started
	<-tr.handlerStarted
	drainCtx, drainCancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer drainCancel()
	err := q.Drain(drainCtx)
	close(tr.returnSubscription)
	select {
	case <-tr.sub.draining:
	case <-ctx.Done():
		t.Fatal("late subscription was not drained")
	}
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, handlerContext.Err(), "graceful drain canceled an already delivered handler")
	require.NoError(t, q.Close())
	require.ErrorIs(t, <-subscribed, ErrClosed)
	require.True(t, tr.sub.forced.Load())
}
