package idem

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRawKeyByteLimitAcrossEntryPoints(t *testing.T) {
	t.Run("Execute and Consume", func(t *testing.T) {
		component, err := New(
			&Config{Driver: DriverMemory},
			WithMaxKeyBytes(4),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		called := false
		_, err = component.Execute(t.Context(), "ééx", func(context.Context) (any, error) {
			called = true
			return "unreachable", nil
		})
		require.ErrorIs(t, err, ErrKeyTooLong)
		require.False(t, called)

		executed, err := component.Consume(t.Context(), "12345", time.Minute, func(context.Context) error {
			called = true
			return nil
		})
		require.ErrorIs(t, err, ErrKeyTooLong)
		require.False(t, executed)
		require.False(t, called)

		result, err := component.Execute(t.Context(), "éé", func(context.Context) (any, error) {
			return "accepted", nil
		})
		require.NoError(t, err)
		require.Equal(t, "accepted", result)
	})

	t.Run("HTTP", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		component, err := New(&Config{Driver: DriverMemory}, WithMaxKeyBytes(4))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		var calls atomic.Int32
		router := gin.New()
		router.Use(component.GinMiddleware())
		router.POST("/limited", func(c *gin.Context) {
			calls.Add(1)
			c.Status(http.StatusNoContent)
		})

		request := httptest.NewRequest(http.MethodPost, "/limited", nil)
		request.Header.Set("X-Idempotency-Key", "12345")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusBadRequest, response.Code)
		require.Zero(t, calls.Load())
	})

	t.Run("gRPC", func(t *testing.T) {
		component, err := New(&Config{Driver: DriverMemory}, WithMaxKeyBytes(4))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		var calls atomic.Int32
		interceptor := component.UnaryServerInterceptor()
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-idem-key", "12345"))
		_, err = interceptor(
			ctx,
			wrapperspb.String("request"),
			&grpc.UnaryServerInfo{FullMethod: "/test.Service/Limited"},
			func(context.Context, any) (any, error) {
				calls.Add(1)
				return wrapperspb.String("unreachable"), nil
			},
		)

		require.Equal(t, codes.InvalidArgument, status.Code(err))
		require.Zero(t, calls.Load())
	})
}

func TestHTTPKeyedRequestBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	component, err := New(&Config{Driver: DriverMemory})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })

	var calls atomic.Int32
	router := gin.New()
	router.Use(component.GinMiddleware(WithHTTPMaxRequestBytes(4)))
	router.POST("/body", func(c *gin.Context) {
		calls.Add(1)
		body, readErr := io.ReadAll(c.Request.Body)
		if readErr != nil {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		c.String(http.StatusOK, string(body))
	})

	knownTooLarge := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader("12345"))
	knownTooLarge.Header.Set("X-Idempotency-Key", "known-too-large")
	knownResponse := httptest.NewRecorder()
	router.ServeHTTP(knownResponse, knownTooLarge)
	require.Equal(t, http.StatusRequestEntityTooLarge, knownResponse.Code)
	require.Zero(t, calls.Load())
	require.Empty(t, component.(*idem).store.(*memoryStore).entries)

	unknownTooLarge := httptest.NewRequest(http.MethodPost, "/body", nil)
	unknownTooLarge.Body = io.NopCloser(strings.NewReader("12345"))
	unknownTooLarge.ContentLength = -1
	unknownTooLarge.Header.Set("X-Idempotency-Key", "unknown-too-large")
	unknownResponse := httptest.NewRecorder()
	router.ServeHTTP(unknownResponse, unknownTooLarge)
	require.Equal(t, http.StatusRequestEntityTooLarge, unknownResponse.Code)
	require.Zero(t, calls.Load())
	require.Empty(t, component.(*idem).store.(*memoryStore).entries)

	readFailure := httptest.NewRequest(http.MethodPost, "/body", nil)
	readFailure.Body = failingReadCloser{}
	readFailure.ContentLength = -1
	readFailure.Header.Set("X-Idempotency-Key", "read-failure")
	readFailureResponse := httptest.NewRecorder()
	router.ServeHTTP(readFailureResponse, readFailure)
	require.Equal(t, http.StatusBadRequest, readFailureResponse.Code)
	require.Zero(t, calls.Load())

	exact := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader("1234"))
	exact.Header.Set("X-Idempotency-Key", "exact")
	exactResponse := httptest.NewRecorder()
	router.ServeHTTP(exactResponse, exact)
	require.Equal(t, http.StatusOK, exactResponse.Code)
	require.Equal(t, "1234", exactResponse.Body.String())
	require.EqualValues(t, 1, calls.Load())

	withoutKey := httptest.NewRequest(http.MethodPost, "/body", strings.NewReader("unbounded-without-key"))
	withoutKeyResponse := httptest.NewRecorder()
	router.ServeHTTP(withoutKeyResponse, withoutKey)
	require.Equal(t, http.StatusOK, withoutKeyResponse.Code)
	require.Equal(t, "unbounded-without-key", withoutKeyResponse.Body.String())
	require.EqualValues(t, 2, calls.Load())
}

func TestOversizedResultsReturnWithoutCaching(t *testing.T) {
	t.Run("Execute preserves normalized result and releases canceled lock", func(t *testing.T) {
		component, err := New(
			&Config{Driver: DriverMemory, LockTTL: time.Minute},
			WithMaxResultBytes(10),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		execute := func(context.Context) (any, error) {
			calls++
			cancel()
			return strings.Repeat("x", 9), nil // JSON encoding is 11 bytes.
		}
		result, err := component.Execute(ctx, "oversized", execute)
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("x", 9), result)
		require.Empty(t, component.(*idem).store.(*memoryStore).locks)

		result, err = component.Execute(t.Context(), "oversized", execute)
		require.NoError(t, err)
		require.Equal(t, strings.Repeat("x", 9), result)
		require.Equal(t, 2, calls)

		cacheableCalls := 0
		cacheable := func(context.Context) (any, error) {
			cacheableCalls++
			return strings.Repeat("y", 8), nil // JSON encoding is exactly 10 bytes.
		}
		_, err = component.Execute(t.Context(), "exact", cacheable)
		require.NoError(t, err)
		_, err = component.Execute(t.Context(), "exact", cacheable)
		require.NoError(t, err)
		require.Equal(t, 1, cacheableCalls)
	})

	t.Run("HTTP streams the complete response and releases its lock", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		component, err := New(
			&Config{Driver: DriverMemory, LockTTL: time.Minute},
			WithMaxResultBytes(8),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		var calls atomic.Int32
		router := gin.New()
		router.Use(component.GinMiddleware())
		router.POST("/stream", func(c *gin.Context) {
			calls.Add(1)
			c.Status(http.StatusOK)
			for _, chunk := range []string{"abcd", "efgh", "ijkl"} {
				_, _ = c.Writer.WriteString(chunk)
				c.Writer.Flush()
			}
		})

		for range 2 {
			request := httptest.NewRequest(http.MethodPost, "/stream", nil)
			request.Header.Set("X-Idempotency-Key", "stream")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, "abcdefghijkl", response.Body.String())
			require.Empty(t, component.(*idem).store.(*memoryStore).locks)
		}
		require.EqualValues(t, 2, calls.Load())
	})

	t.Run("gRPC returns the complete response", func(t *testing.T) {
		component, err := New(
			&Config{Driver: DriverMemory},
			WithMaxResultBytes(32),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		var calls atomic.Int32
		interceptor := component.UnaryServerInterceptor()
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-idem-key", "large-response"))
		info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Large"}
		handler := func(context.Context, any) (any, error) {
			calls.Add(1)
			return wrapperspb.String(strings.Repeat("z", 128)), nil
		}

		for range 2 {
			result, callErr := interceptor(ctx, wrapperspb.String("request"), info, handler)
			require.NoError(t, callErr)
			require.Equal(t, strings.Repeat("z", 128), result.(*wrapperspb.StringValue).Value)
		}
		require.EqualValues(t, 2, calls.Load())
	})
}

func TestLimitedCaptureIsLazyBoundedAndTracksActualWrites(t *testing.T) {
	capture := newLimitedCapture(4)
	capture.Write([]byte("abc"))
	capture.WriteString("def")
	require.Equal(t, "abcd", string(capture.Bytes()))
	require.True(t, capture.Exceeded())
	require.LessOrEqual(t, capture.Capacity(), 4)

	defaultCapture := newLimitedCapture(defaultMaxResultBytes)
	defaultCapture.Write([]byte("x"))
	require.LessOrEqual(t, defaultCapture.Capacity(), minCaptureChunkBytes)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	partial := &partialGinResponseWriter{ResponseWriter: ctx.Writer, maxWrite: 3}
	actualCapture := newLimitedCapture(16)
	writer := &responseWriter{ResponseWriter: partial, body: actualCapture}

	n, err := writer.Write([]byte("abcdef"))
	require.NoError(t, err)
	require.Equal(t, 3, n)
	n, err = writer.WriteString("WXYZ")
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, "abcWXY", recorder.Body.String())
	require.Equal(t, "abcWXY", string(actualCapture.Bytes()))
}

func TestMemoryStoreCapacityAndExpiration(t *testing.T) {
	store := newMemoryStoreWithCleanupAndLimit("capacity:", time.Hour, 1).(*memoryStore)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	token, locked, err := store.Lock(t.Context(), "first", time.Minute)
	require.NoError(t, err)
	require.True(t, locked)

	_, _, err = store.Lock(t.Context(), "second", time.Minute)
	require.ErrorIs(t, err, ErrStoreCapacity)

	// A successful Lock reserves the result slot; lock-to-result conversion
	// remains valid even while the store is at capacity.
	require.NoError(t, store.SetResult(t.Context(), "first", []byte("one"), time.Minute, token))
	require.Len(t, store.entries, 1)
	require.Empty(t, store.locks)
	require.Len(t, store.results, 1)

	// Existing keys can be updated while a different new key is rejected.
	require.NoError(t, store.SetResult(t.Context(), "first", []byte("updated"), time.Minute, ""))
	require.ErrorIs(t, store.SetResult(t.Context(), "second", []byte("two"), time.Minute, ""), ErrStoreCapacity)

	require.NoError(t, store.DeleteResult(t.Context(), "first"))
	token, locked, err = store.Lock(t.Context(), "second", time.Minute)
	require.NoError(t, err)
	require.True(t, locked)
	require.NoError(t, store.Unlock(t.Context(), "second", token))
	require.Empty(t, store.entries)

	_, locked, err = store.Lock(t.Context(), "expired", time.Minute)
	require.NoError(t, err)
	require.True(t, locked)
	store.mu.Lock()
	expired := store.locks[store.prefix+"expired"+lockSuffix]
	expired.expiresAt = time.Now().Add(-time.Second)
	store.locks[store.prefix+"expired"+lockSuffix] = expired
	store.mu.Unlock()

	replacementToken, locked, err := store.Lock(t.Context(), "replacement", time.Minute)
	require.NoError(t, err)
	require.True(t, locked)
	require.Len(t, store.entries, 1)
	_, hasExpired := store.entries[store.prefix+"expired"]
	require.False(t, hasExpired)

	require.NoError(t, store.SetResult(t.Context(), "replacement", []byte("result"), time.Minute, replacementToken))
	store.mu.Lock()
	expiredResult := store.results[store.prefix+"replacement"+resultSuffix]
	expiredResult.expiresAt = time.Now().Add(-time.Second)
	store.results[store.prefix+"replacement"+resultSuffix] = expiredResult
	store.mu.Unlock()

	_, locked, err = store.Lock(t.Context(), "after-result-expiry", time.Minute)
	require.NoError(t, err)
	require.True(t, locked)
	require.Len(t, store.entries, 1)
}

func TestMemoryStoreCapacityRace(t *testing.T) {
	const (
		capacity = 8
		workers  = 64
	)
	store := newMemoryStoreWithCleanupAndLimit("capacity-race:", time.Hour, capacity).(*memoryStore)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	start := make(chan struct{})
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for n := range workers {
		wg.Go(func() {
			<-start
			_, locked, err := store.Lock(t.Context(), "key-"+strconv.Itoa(n), time.Minute)
			if err == nil && !locked {
				err = errors.New("unique key was neither locked nor rejected")
			}
			results <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)

	accepted := 0
	rejected := 0
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, ErrStoreCapacity):
			rejected++
		default:
			t.Fatalf("unexpected capacity race error: %v", err)
		}
	}
	require.Equal(t, capacity, accepted)
	require.Equal(t, workers-capacity, rejected)
	store.mu.Lock()
	entryCount := len(store.entries)
	lockCount := len(store.locks)
	store.mu.Unlock()
	require.Equal(t, capacity, entryCount)
	require.Equal(t, capacity, lockCount)
}

func TestCapacityErrorsPrecedeBusinessAndMapToProtocols(t *testing.T) {
	t.Run("Execute and Consume", func(t *testing.T) {
		component, err := New(
			&Config{Driver: DriverMemory},
			WithMemoryMaxEntries(1),
		)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })

		_, err = component.Execute(t.Context(), "occupied", func(context.Context) (any, error) {
			return "cached", nil
		})
		require.NoError(t, err)

		called := false
		_, err = component.Execute(t.Context(), "new", func(context.Context) (any, error) {
			called = true
			return "unreachable", nil
		})
		require.ErrorIs(t, err, ErrStoreCapacity)
		require.False(t, called)

		executed, err := component.Consume(t.Context(), "consume", time.Minute, func(context.Context) error {
			called = true
			return nil
		})
		require.ErrorIs(t, err, ErrStoreCapacity)
		require.False(t, executed)
		require.False(t, called)
	})

	t.Run("HTTP", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		component, err := New(&Config{Driver: DriverMemory}, WithMemoryMaxEntries(1))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })
		_, err = component.Execute(t.Context(), "occupied", func(context.Context) (any, error) {
			return "cached", nil
		})
		require.NoError(t, err)

		var calls atomic.Int32
		router := gin.New()
		router.Use(component.GinMiddleware())
		router.POST("/capacity", func(c *gin.Context) {
			calls.Add(1)
			c.Status(http.StatusNoContent)
		})
		request := httptest.NewRequest(http.MethodPost, "/capacity", nil)
		request.Header.Set("X-Idempotency-Key", "new")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusServiceUnavailable, response.Code)
		require.Zero(t, calls.Load())
	})

	t.Run("gRPC", func(t *testing.T) {
		component, err := New(&Config{Driver: DriverMemory}, WithMemoryMaxEntries(1))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, component.Close()) })
		_, err = component.Execute(t.Context(), "occupied", func(context.Context) (any, error) {
			return "cached", nil
		})
		require.NoError(t, err)

		var calls atomic.Int32
		interceptor := component.UnaryServerInterceptor()
		ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-idem-key", "new"))
		_, err = interceptor(
			ctx,
			wrapperspb.String("request"),
			&grpc.UnaryServerInfo{FullMethod: "/test.Service/Capacity"},
			func(context.Context, any) (any, error) {
				calls.Add(1)
				return wrapperspb.String("unreachable"), nil
			},
		)

		require.Equal(t, codes.ResourceExhausted, status.Code(err))
		require.Zero(t, calls.Load())
	})
}

func TestSameKeyWaiterDoesNotFailAtCapacity(t *testing.T) {
	component, err := New(
		&Config{Driver: DriverMemory, WaitTimeout: 2 * time.Second},
		WithMemoryMaxEntries(1),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })

	started := make(chan struct{})
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	var calls atomic.Int32
	go func() {
		_, executeErr := component.Execute(t.Context(), "same", func(context.Context) (any, error) {
			calls.Add(1)
			close(started)
			<-release
			return "done", nil
		})
		firstResult <- executeErr
	}()
	<-started

	secondResult := make(chan error, 1)
	go func() {
		result, executeErr := component.Execute(t.Context(), "same", func(context.Context) (any, error) {
			calls.Add(1)
			return "duplicate", nil
		})
		if executeErr == nil && result != "done" {
			executeErr = errors.New("waiter did not receive the completed result")
		}
		secondResult <- executeErr
	}()

	close(release)
	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	require.EqualValues(t, 1, calls.Load())
}

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (failingReadCloser) Close() error {
	return nil
}

type partialGinResponseWriter struct {
	gin.ResponseWriter
	maxWrite int
}

func (w *partialGinResponseWriter) Write(p []byte) (int, error) {
	return w.ResponseWriter.Write(p[:min(len(p), w.maxWrite)])
}

func (w *partialGinResponseWriter) WriteString(s string) (int, error) {
	return w.ResponseWriter.WriteString(s[:min(len(s), w.maxWrite)])
}
