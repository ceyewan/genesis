package breaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNew_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *Config
	}{
		{
			name: "negative interval",
			cfg: &Config{
				Interval: -1 * time.Second,
			},
		},
		{
			name: "negative timeout",
			cfg: &Config{
				Timeout: -1 * time.Second,
			},
		},
		{
			name: "failure ratio less than zero",
			cfg: &Config{
				FailureRatio: -0.1,
			},
		},
		{
			name: "failure ratio greater than one",
			cfg: &Config{
				FailureRatio: 1.1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			brk, err := New(tt.cfg)
			require.Nil(t, brk)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidConfig)
		})
	}
}

func TestUnaryClientInterceptor_GRPCFailureClassification(t *testing.T) {
	t.Run("business errors should not trip breaker", func(t *testing.T) {
		brk, err := New(&Config{
			MaxRequests:     1,
			Timeout:         time.Second,
			FailureRatio:    0.5,
			MinimumRequests: 2,
		})
		require.NoError(t, err)

		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "svc-business"
		}))

		businessErr := status.Error(codes.InvalidArgument, "invalid request")
		for range 10 {
			err = interceptor(context.Background(), "/svc.Method", "req", "reply", nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return businessErr
			})
			require.ErrorIs(t, err, businessErr)
		}

		state, err := brk.State("svc-business")
		require.NoError(t, err)
		require.Equal(t, StateClosed, state)
	})

	t.Run("transport errors should trip breaker", func(t *testing.T) {
		brk, err := New(&Config{
			MaxRequests:     1,
			Timeout:         time.Second,
			FailureRatio:    0.5,
			MinimumRequests: 2,
		})
		require.NoError(t, err)

		interceptor := brk.UnaryClientInterceptor(WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return "svc-transport"
		}))

		transportErr := status.Error(codes.Unavailable, "service unavailable")
		for range 2 {
			err = interceptor(context.Background(), "/svc.Method", "req", "reply", nil, func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return transportErr
			})
			require.ErrorIs(t, err, transportErr)
		}

		state, err := brk.State("svc-transport")
		require.NoError(t, err)
		require.Equal(t, StateOpen, state)
	})
}

func TestExecute_UsesFallbackForHalfOpenRejection(t *testing.T) {
	testErr := errors.New("downstream failed")
	fallbackErr := errors.New("fallback handled rejection")

	fallbackCalled := make(chan error, 1)
	brk, err := New(&Config{
		MaxRequests:     1,
		Timeout:         50 * time.Millisecond,
		FailureRatio:    0.5,
		MinimumRequests: 2,
	}, WithFallback(func(ctx context.Context, key string, err error) error {
		fallbackCalled <- err
		return fallbackErr
	}))
	require.NoError(t, err)

	ctx := context.Background()
	for range 2 {
		_, execErr := brk.Execute(ctx, "svc-half-open", func() (any, error) {
			return nil, testErr
		})
		require.ErrorIs(t, execErr, testErr)
	}

	state, err := brk.State("svc-half-open")
	require.NoError(t, err)
	require.Equal(t, StateOpen, state)

	time.Sleep(80 * time.Millisecond)

	probeStarted := make(chan struct{})
	probeRelease := make(chan struct{})
	probeDone := make(chan error, 1)

	go func() {
		_, execErr := brk.Execute(ctx, "svc-half-open", func() (any, error) {
			close(probeStarted)
			<-probeRelease
			return "ok", nil
		})
		probeDone <- execErr
	}()

	<-probeStarted

	_, err = brk.Execute(ctx, "svc-half-open", func() (any, error) {
		return "unexpected", nil
	})
	require.ErrorIs(t, err, fallbackErr)

	select {
	case rejectionErr := <-fallbackCalled:
		require.ErrorIs(t, rejectionErr, ErrTooManyRequests)
	case <-time.After(time.Second):
		t.Fatal("fallback should be called for half-open rejection")
	}

	close(probeRelease)
	require.NoError(t, <-probeDone)
}
