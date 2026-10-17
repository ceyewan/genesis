package idem

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Refresh remains in flight until stopRefresh joins it, then reports a real
// ownership failure. No frontend may commit a result before observing it.
type completionRefreshStore struct {
	Store
	started chan struct{}
	writes  atomic.Int32
}

func (s *completionRefreshStore) Refresh(ctx context.Context, _ string, _ LockToken, _ time.Duration) error {
	close(s.started)
	<-ctx.Done()
	return ErrLockLost
}

func (s *completionRefreshStore) SetResult(ctx context.Context, key string, val []byte, ttl time.Duration, token LockToken) error {
	s.writes.Add(1)
	return s.Store.SetResult(ctx, key, val, ttl, token)
}

func TestCompletionJoinsRefreshBeforeCommit(t *testing.T) {
	for _, frontend := range []string{"Execute", "Consume", "HTTP", "gRPC"} {
		t.Run(frontend, func(t *testing.T) {
			store := newMemoryStore("refresh:")
			t.Cleanup(func() { require.NoError(t, store.(closableStore).Close()) })
			gated := &completionRefreshStore{Store: store, started: make(chan struct{})}
			i := newIdempotency(&Config{DefaultTTL: time.Minute, LockTTL: 60 * time.Millisecond}, gated)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			business := func() {
				select {
				case <-gated.started:
				case <-ctx.Done():
					t.Fatal("Refresh did not start")
				}
			}
			switch frontend {
			case "Execute":
				_, err := i.Execute(ctx, "key", func(context.Context) (any, error) { business(); return "ok", nil })
				require.ErrorIs(t, err, ErrLockLost)
			case "Consume":
				_, err := i.Consume(ctx, "key", time.Minute, func(context.Context) error { business(); return nil })
				require.ErrorIs(t, err, ErrLockLost)
			case "HTTP":
				r := gin.New()
				r.Use(i.GinMiddleware())
				r.POST("/test", func(c *gin.Context) { business(); c.String(http.StatusOK, "ok") })
				req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/test", nil)
				req.Header.Set("X-Idempotency-Key", "key")
				r.ServeHTTP(httptest.NewRecorder(), req)
			case "gRPC":
				ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-idem-key", "key"))
				_, err := i.UnaryServerInterceptor()(ctx, "req", &grpc.UnaryServerInfo{FullMethod: "/test/Call"}, func(context.Context, any) (any, error) {
					business()
					return wrapperspb.String("ok"), nil
				})
				require.ErrorIs(t, err, ErrLockLost)
			}
			require.Zero(t, gated.writes.Load())
		})
	}
}

func TestGinMiddlewareRestoresRequestContext(t *testing.T) {
	i, err := New(&Config{Driver: DriverMemory})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, i.Close()) })
	r := gin.New()
	r.Use(func(c *gin.Context) {
		original := c.Request.Context()
		c.Next()
		require.Same(t, original, c.Request.Context())
		require.NoError(t, c.Request.Context().Err())
	})
	r.Use(i.GinMiddleware())
	r.POST("/test", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/test", nil)
	req.Header.Set("X-Idempotency-Key", "key")
	r.ServeHTTP(httptest.NewRecorder(), req)
}

func TestSubmillisecondTTLRejected(t *testing.T) {
	t.Parallel()
	for _, cfg := range []*Config{
		{Driver: DriverMemory, DefaultTTL: time.Microsecond},
		{Driver: DriverMemory, LockTTL: time.Microsecond},
	} {
		_, err := New(cfg)
		require.ErrorIs(t, err, ErrInvalidConfig)
	}
	i, err := New(&Config{Driver: DriverMemory})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, i.Close()) })
	_, err = i.Consume(t.Context(), "key", time.Microsecond, func(context.Context) error { t.Fatal("called"); return nil })
	require.ErrorIs(t, err, ErrInvalidConfig)
}
