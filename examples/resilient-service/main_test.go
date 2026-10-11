package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ceyewan/genesis/breaker"
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/ratelimit"
)

func testGateway(t *testing.T, limit ratelimit.Limit, timeout time.Duration) *gateway {
	t.Helper()
	g, err := newGateway(clog.Discard(), limit, &breaker.Config{MinimumRequests: 3, FailureRatio: 0.5, Timeout: timeout, MaxRequests: 1})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, g.close()) })
	return g
}

func orderRequest(g *gateway, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/orders", nil)
	request.Header.Set("X-Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	g.router.ServeHTTP(recorder, request)
	return recorder
}

func TestConcurrentIdempotentOrders(t *testing.T) {
	t.Parallel()
	g := testGateway(t, ratelimit.Limit{Rate: 1000, Burst: 100}, time.Second)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 20)
	for range 20 {
		wg.Go(func() { results <- orderRequest(g, "same-order") })
	}
	wg.Wait()
	close(results)
	var body string
	for result := range results {
		require.Equal(t, http.StatusCreated, result.Code)
		if body == "" {
			body = result.Body.String()
		}
		require.Equal(t, body, result.Body.String())
	}
	require.EqualValues(t, 1, g.orders.Load())
	require.EqualValues(t, 1, g.inventory.calls.Load())
}

func TestRateLimitAndRecovery(t *testing.T) {
	t.Parallel()
	g := testGateway(t, ratelimit.Limit{Rate: 20, Burst: 1}, time.Second)
	require.Equal(t, http.StatusCreated, orderRequest(g, "first").Code)
	require.Equal(t, http.StatusTooManyRequests, orderRequest(g, "first").Code, "Cache hits still consume admission tokens")
	require.Eventually(t, func() bool { return orderRequest(g, "second").Code == http.StatusCreated }, time.Second, 10*time.Millisecond)
}

func TestBreakerFailureAndHalfOpenRecovery(t *testing.T) {
	t.Parallel()
	g := testGateway(t, ratelimit.Limit{Rate: 1000, Burst: 100}, 200*time.Millisecond)
	g.inventory.mode.Store(1)
	for i := range 3 {
		require.Equal(t, http.StatusServiceUnavailable, orderRequest(g, fmt.Sprintf("failed-%d", i)).Code)
	}
	state, err := g.breaker.State(g.conn.Target())
	require.NoError(t, err)
	require.Equal(t, breaker.StateOpen, state)
	calls := g.inventory.calls.Load()
	require.Equal(t, http.StatusServiceUnavailable, orderRequest(g, "rejected").Code)
	require.Equal(t, calls, g.inventory.calls.Load())
	g.inventory.mode.Store(0)
	require.Eventually(t, func() bool {
		state, err := g.breaker.State(g.conn.Target())
		return err == nil && state == breaker.StateHalfOpen
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, http.StatusCreated, orderRequest(g, "recovered").Code)
	state, err = g.breaker.State(g.conn.Target())
	require.NoError(t, err)
	require.Equal(t, breaker.StateClosed, state)
}

func TestBusinessFailuresDoNotOpenBreaker(t *testing.T) {
	t.Parallel()
	g := testGateway(t, ratelimit.Limit{Rate: 1000, Burst: 100}, time.Second)
	g.inventory.mode.Store(2)
	for i := range 5 {
		require.Equal(t, http.StatusConflict, orderRequest(g, fmt.Sprintf("business-%d", i)).Code)
	}
	state, err := g.breaker.State(g.conn.Target())
	require.NoError(t, err)
	require.Equal(t, breaker.StateClosed, state)
	require.EqualValues(t, 5, g.inventory.calls.Load())
	require.Zero(t, g.orders.Load())
}
