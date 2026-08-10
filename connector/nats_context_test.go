package connector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

type natsAttemptInfo struct {
	timeout time.Duration
	err     error
}

func TestNATSConnectHonorsCallerCancellationAndDeadline(t *testing.T) {
	conn, err := NewNATS(&NATSConfig{
		URL:            "nats://127.0.0.1:4222",
		ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	impl := conn.(*natsConnector)

	attempted := make(chan natsAttemptInfo, 1)
	release := make(chan struct{})
	finished := make(chan struct{})
	impl.connect = func(_ string, opts ...nats.Option) (*nats.Conn, error) {
		defer close(finished)
		var parsed nats.Options
		for _, opt := range opts {
			if optionErr := opt(&parsed); optionErr != nil {
				attempted <- natsAttemptInfo{err: optionErr}
				return nil, optionErr
			}
		}
		attempted <- natsAttemptInfo{timeout: parsed.Timeout}
		<-release
		return nil, errors.New("test attempt released")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	done := make(chan error, 1)
	go func() { done <- conn.Connect(ctx) }()

	info := <-attempted
	if info.err != nil {
		t.Fatal(info.err)
	}
	if info.timeout <= 0 || info.timeout >= 5*time.Second || info.timeout > time.Second {
		t.Fatalf("nats timeout = %v, want positive timeout bounded by caller deadline", info.timeout)
	}

	cancel()
	select {
	case connectErr := <-done:
		if !errors.Is(connectErr, ErrConnection) {
			t.Fatalf("Connect() error = %v, want ErrConnection", connectErr)
		}
		if !errors.Is(connectErr, context.Canceled) {
			t.Fatalf("Connect() error = %v, want context.Canceled", connectErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect() did not return after caller cancellation")
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("abandoned NATS attempt did not finish")
	}
}

func TestNATSConnectRejectsAlreadyCanceledContextWithoutDialing(t *testing.T) {
	t.Parallel()

	conn, err := NewNATS(&NATSConfig{URL: "nats://127.0.0.1:4222"})
	if err != nil {
		t.Fatal(err)
	}
	impl := conn.(*natsConnector)
	attempted := false
	impl.connect = func(string, ...nats.Option) (*nats.Conn, error) {
		attempted = true
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = conn.Connect(ctx)
	if !errors.Is(err, ErrConnection) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Connect() error = %v, want ErrConnection and context.Canceled", err)
	}
	if attempted {
		t.Fatal("Connect() dialed NATS with an already canceled context")
	}
}

func TestNATSConnectReturnsCallerDeadline(t *testing.T) {
	t.Parallel()

	conn, err := NewNATS(&NATSConfig{
		URL:            "nats://127.0.0.1:4222",
		ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	impl := conn.(*natsConnector)
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	impl.connect = func(string, ...nats.Option) (*nats.Conn, error) {
		close(started)
		defer close(finished)
		<-release
		return nil, errors.New("test attempt released")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- conn.Connect(ctx) }()
	<-started

	select {
	case connectErr := <-done:
		if !errors.Is(connectErr, ErrConnection) || !errors.Is(connectErr, context.DeadlineExceeded) {
			t.Fatalf("Connect() error = %v, want ErrConnection and context.DeadlineExceeded", connectErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Connect() ignored caller deadline")
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("abandoned NATS attempt did not finish")
	}
}
