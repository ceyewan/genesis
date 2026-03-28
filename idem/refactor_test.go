package idem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestExecute_ReturnTypeStable(t *testing.T) {
	t.Parallel()

	idemComp, err := New(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:type-stable:",
		DefaultTTL: time.Minute,
		LockTTL:    time.Second,
	})
	require.NoError(t, err)

	type order struct {
		OrderID string `json:"order_id"`
		Amount  int    `json:"amount"`
	}

	ctx := context.Background()
	key := "create:order:1"

	result1, err := idemComp.Execute(ctx, key, func(ctx context.Context) (any, error) {
		return order{OrderID: "ord-1", Amount: 42}, nil
	})
	require.NoError(t, err)

	result2, err := idemComp.Execute(ctx, key, func(ctx context.Context) (any, error) {
		return order{OrderID: "ord-2", Amount: 99}, nil
	})
	require.NoError(t, err)

	first, ok := result1.(map[string]any)
	require.True(t, ok)
	second, ok := result2.(map[string]any)
	require.True(t, ok)

	require.Equal(t, first, second)
	require.Equal(t, "ord-1", first["order_id"])
	require.Equal(t, float64(42), first["amount"])
}

func TestExecute_RecoversFromCorruptedCachedResult(t *testing.T) {
	t.Parallel()

	store := newMemoryStore("test:idem:corrupt:").(Store)
	idemComp := newIdempotency(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:corrupt:",
		DefaultTTL: time.Minute,
		LockTTL:    time.Second,
	}, store, nil)

	ctx := context.Background()
	key := "corrupt-key"

	err := store.SetResult(ctx, key, []byte("{not-json"), time.Minute, "")
	require.NoError(t, err)

	called := 0
	result, err := idemComp.Execute(ctx, key, func(ctx context.Context) (any, error) {
		called++
		return map[string]any{"status": "rebuilt"}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, called)

	value, ok := result.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "rebuilt", value["status"])
}

func TestNew_InvalidConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  *Config
	}{
		{
			name: "negative default ttl",
			cfg:  &Config{Driver: DriverMemory, DefaultTTL: -time.Second},
		},
		{
			name: "negative lock ttl",
			cfg:  &Config{Driver: DriverMemory, LockTTL: -time.Second},
		},
		{
			name: "negative wait timeout",
			cfg:  &Config{Driver: DriverMemory, WaitTimeout: -time.Second},
		},
		{
			name: "negative wait interval",
			cfg:  &Config{Driver: DriverMemory, WaitInterval: -time.Millisecond},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			idemComp, err := New(tt.cfg)
			require.Nil(t, idemComp)
			require.Error(t, err)
		})
	}
}

func TestGinMiddleware_CustomCacheStrategy(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	idemComp, err := New(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:http-cache:",
		DefaultTTL: time.Minute,
		LockTTL:    time.Second,
	})
	require.NoError(t, err)

	router := gin.New()
	router.Use(gin.HandlerFunc(idemComp.GinMiddleware(
		WithHTTPStatusCacheFunc(func(status int) bool {
			return status == http.StatusConflict
		}),
	).(func(*gin.Context))))

	calls := 0
	router.POST("/conflict", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusConflict, gin.H{"call": calls})
	})

	req1 := httptest.NewRequest(http.MethodPost, "/conflict", nil)
	req1.Header.Set("X-Idempotency-Key", "http-conflict")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusConflict, w1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/conflict", nil)
	req2.Header.Set("X-Idempotency-Key", "http-conflict")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusConflict, w2.Code)
	require.Equal(t, 1, calls)
	require.Equal(t, w1.Body.String(), w2.Body.String())
}

func TestUnaryServerInterceptor_CustomCacheStrategy(t *testing.T) {
	t.Parallel()

	idemComp, err := New(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:grpc-cache:",
		DefaultTTL: time.Minute,
		LockTTL:    time.Second,
	})
	require.NoError(t, err)

	interceptor := idemComp.UnaryServerInterceptor(WithGRPCResponseCacheFunc(func(msg proto.Message) bool {
		return false
	}))

	count := 0
	handler := func(ctx context.Context, req any) (any, error) {
		count++
		return wrapperspb.String("success"), nil
	}

	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", "grpc-no-cache"))

	resp1, err := interceptor(ctx, "req", info, handler)
	require.NoError(t, err)
	resp2, err := interceptor(ctx, "req", info, handler)
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Equal(t, resp1, resp2)
}

func TestExecute_ReturnsLockLostOnRefreshFailure(t *testing.T) {
	t.Parallel()

	store := &refreshFailStore{Store: newMemoryStore("test:idem:refresh-fail:")}
	idemComp := newIdempotency(&Config{
		Driver:     DriverMemory,
		Prefix:     "test:idem:refresh-fail:",
		DefaultTTL: time.Minute,
		LockTTL:    time.Second,
	}, store, nil)

	_, err := idemComp.Execute(context.Background(), "refresh-fail", func(ctx context.Context) (any, error) {
		time.Sleep(650 * time.Millisecond)
		return map[string]any{"status": "ok"}, nil
	})
	require.ErrorIs(t, err, ErrLockLost)

	_, getErr := store.GetResult(context.Background(), "refresh-fail")
	require.ErrorIs(t, getErr, ErrResultNotFound)
}

type refreshFailStore struct {
	Store
	failed bool
}

func (s *refreshFailStore) Refresh(ctx context.Context, key string, token LockToken, ttl time.Duration) error {
	if s.failed {
		return nil
	}
	s.failed = true
	return ErrLockLost
}

func (s *refreshFailStore) DeleteResult(ctx context.Context, key string) error {
	ds, ok := s.Store.(DeletableStore)
	if !ok {
		return errors.New("store does not support delete")
	}
	return ds.DeleteResult(ctx, key)
}
