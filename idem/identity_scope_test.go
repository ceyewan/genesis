package idem

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestGinMiddlewareIdentityScopePreventsCrossTenantResultReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	component, err := New(&Config{Driver: DriverMemory, DefaultTTL: time.Minute, LockTTL: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })

	router := gin.New()
	// This test middleware represents authentication: the idem callback reads
	// the verified identity from Gin context, not from the request directly.
	router.Use(func(c *gin.Context) {
		c.Set("verified_identity", c.GetHeader("X-Test-Verified-Identity"))
		c.Next()
	})
	router.Use(component.GinMiddleware(WithHTTPIdentityScopeFunc(func(c *gin.Context) (string, error) {
		return c.GetString("verified_identity"), nil
	})))

	calls := make(map[string]int)
	router.POST("/orders", func(c *gin.Context) {
		identity := c.GetString("verified_identity")
		calls[identity]++
		c.JSON(http.StatusOK, gin.H{"identity": identity})
	})

	request := func(identity string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"item":1}`))
		req.Header.Set("X-Idempotency-Key", "shared-client-key")
		req.Header.Set("X-Test-Verified-Identity", identity)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	tenantA1 := request("tenant-a\x00principal-1")
	tenantB1 := request("tenant-b\x00principal-1")
	tenantA2 := request("tenant-a\x00principal-1")
	tenantB2 := request("tenant-b\x00principal-1")

	require.Equal(t, http.StatusOK, tenantA1.Code)
	require.Equal(t, http.StatusOK, tenantB1.Code)
	require.JSONEq(t, `{"identity":"tenant-a\u0000principal-1"}`, tenantA2.Body.String())
	require.JSONEq(t, `{"identity":"tenant-b\u0000principal-1"}`, tenantB2.Body.String())
	require.Equal(t, 1, calls["tenant-a\x00principal-1"])
	require.Equal(t, 1, calls["tenant-b\x00principal-1"])
}

func TestGinMiddlewareIdentityScopeFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	component, err := New(&Config{Driver: DriverMemory})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })

	handlerCalled := false
	router := gin.New()
	router.Use(component.GinMiddleware(WithHTTPIdentityScopeFunc(func(*gin.Context) (string, error) {
		return "", errors.New("identity unavailable")
	})))
	router.POST("/orders", func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/orders", nil)
	req.Header.Set("X-Idempotency-Key", "key")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusUnauthorized, resp.Code)
	require.False(t, handlerCalled)
}

func TestUnaryServerInterceptorIdentityScopePreventsCrossTenantResultReuse(t *testing.T) {
	component, err := New(&Config{Driver: DriverMemory, DefaultTTL: time.Minute, LockTTL: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })

	interceptor := component.UnaryServerInterceptor(WithGRPCIdentityScopeFunc(func(ctx context.Context) (string, error) {
		identity, _ := ctx.Value(verifiedIdentityContextKey{}).(string)
		return identity, nil
	}))
	info := &grpc.UnaryServerInfo{FullMethod: "/orders.OrderService/Create"}
	calls := make(map[string]int)
	handler := func(ctx context.Context, _ any) (any, error) {
		identity, _ := ctx.Value(verifiedIdentityContextKey{}).(string)
		calls[identity]++
		return wrapperspb.String(identity), nil
	}
	call := func(identity string) *wrapperspb.StringValue {
		ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", "shared-client-key"))
		ctx = context.WithValue(ctx, verifiedIdentityContextKey{}, identity)
		result, callErr := interceptor(ctx, wrapperspb.String("same-request"), info, handler)
		require.NoError(t, callErr)
		message, ok := result.(*wrapperspb.StringValue)
		require.True(t, ok)
		return message
	}

	require.Equal(t, "tenant-a\x00principal-1", call("tenant-a\x00principal-1").Value)
	require.Equal(t, "tenant-b\x00principal-1", call("tenant-b\x00principal-1").Value)
	require.Equal(t, "tenant-a\x00principal-1", call("tenant-a\x00principal-1").Value)
	require.Equal(t, "tenant-b\x00principal-1", call("tenant-b\x00principal-1").Value)
	require.Equal(t, 1, calls["tenant-a\x00principal-1"])
	require.Equal(t, 1, calls["tenant-b\x00principal-1"])
}

func TestUnaryServerInterceptorIdentityScopeFailsClosed(t *testing.T) {
	component, err := New(&Config{Driver: DriverMemory})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, component.Close()) })

	interceptor := component.UnaryServerInterceptor(WithGRPCIdentityScopeFunc(func(context.Context) (string, error) {
		return "", nil
	}))
	handlerCalled := false
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-idem-key", "key"))
	_, err = interceptor(ctx, wrapperspb.String("request"), &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}, func(context.Context, any) (any, error) {
		handlerCalled = true
		return wrapperspb.String("response"), nil
	})

	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.False(t, handlerCalled)
}

func TestIdentityScopeBindsFingerprint(t *testing.T) {
	base := "canonical-request-fingerprint"
	tenantA := bindIdempotencyIdentity("http-fingerprint", base, "tenant-a")
	tenantB := bindIdempotencyIdentity("http-fingerprint", base, "tenant-b")
	require.NotEqual(t, tenantA, tenantB)

	envelope, err := encodeIdemEnvelope(tenantA, []byte("response"))
	require.NoError(t, err)
	_, err = decodeIdemEnvelope(envelope, tenantB)
	require.ErrorIs(t, err, ErrKeyConflict)
}

func TestIdentityScopeTupleEncodingIsUnambiguous(t *testing.T) {
	first := bindIdempotencyIdentity("http-key", "c", "a\x00b")
	second := bindIdempotencyIdentity("http-key", "b\x00c", "a")
	require.NotEqual(t, first, second)
}

type verifiedIdentityContextKey struct{}
