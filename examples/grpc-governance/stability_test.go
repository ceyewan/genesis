package main

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type intermittentHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	calls atomic.Int32
}

func (s *intermittentHealthServer) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	if s.calls.Add(1)%2 == 0 {
		return nil, status.Error(codes.Unavailable, "intermittent stale endpoint")
	}
	if err := grpc.SetHeader(ctx, metadata.Pairs("instance-id", "first")); err != nil {
		return nil, err
	}
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func TestDiscoveryRequiresConsecutiveSuccessfulCalls(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	service := &intermittentHealthServer{}
	grpc_health_v1.RegisterHealthServer(server, service)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		require.NoError(t, <-done)
	})
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, conn.Close()) })
	ready, readyCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer readyCancel()
	_, err = grpc_health_v1.NewHealthClient(conn).Check(ready, &grpc_health_v1.HealthCheckRequest{}, grpc.WaitForReady(true))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, waitCallSet(ctx, conn, "first"), context.DeadlineExceeded)
	require.GreaterOrEqual(t, service.calls.Load(), int32(3), "A single successful call must not pass acceptance")
}
