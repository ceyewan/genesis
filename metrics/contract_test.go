package metrics

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestHTTPStatusClassAndOutcome(t *testing.T) {
	tests := []struct {
		status     int
		wantClass  string
		wantResult string
	}{
		{status: 200, wantClass: "2xx", wantResult: OutcomeSuccess},
		{status: 302, wantClass: "3xx", wantResult: OutcomeSuccess},
		{status: 404, wantClass: "4xx", wantResult: OutcomeError},
		{status: 503, wantClass: "5xx", wantResult: OutcomeError},
		{status: 99, wantClass: "unknown", wantResult: OutcomeError},
	}

	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			if got := HTTPStatusClass(tc.status); got != tc.wantClass {
				t.Fatalf("HTTPStatusClass() = %q, want %q", got, tc.wantClass)
			}
			if got := HTTPOutcome(tc.status); got != tc.wantResult {
				t.Fatalf("HTTPOutcome() = %q, want %q", got, tc.wantResult)
			}
		})
	}
}

func TestGRPCStatusClassAndOutcome(t *testing.T) {
	if got := GRPCStatusClass(codes.OK); got != "ok" {
		t.Fatalf("GRPCStatusClass(OK) = %q, want ok", got)
	}
	if got := GRPCStatusClass(codes.InvalidArgument); got != "invalidargument" {
		t.Fatalf("GRPCStatusClass(INVALID_ARGUMENT) = %q, want invalidargument", got)
	}
	if got := GRPCOutcome(codes.OK); got != OutcomeSuccess {
		t.Fatalf("GRPCOutcome(OK) = %q, want %q", got, OutcomeSuccess)
	}
	if got := GRPCOutcome(codes.Internal); got != OutcomeError {
		t.Fatalf("GRPCOutcome(INTERNAL) = %q, want %q", got, OutcomeError)
	}
}

func TestHTTPServerMetricsObserve(t *testing.T) {
	m, err := NewHTTPServerMetrics(Discard(), DefaultHTTPServerMetricsConfig("svc"))
	if err != nil {
		t.Fatalf("NewHTTPServerMetrics() error = %v", err)
	}
	m.Observe(context.Background(), http.MethodPost, "/orders", 200, 10*time.Millisecond)
	m.Observe(context.Background(), "", "", 503, 20*time.Millisecond)
}

func TestGRPCServerMetricsInterceptors(t *testing.T) {
	m, err := NewGRPCServerMetrics(Discard(), DefaultGRPCServerMetricsConfig("svc"))
	if err != nil {
		t.Fatalf("NewGRPCServerMetrics() error = %v", err)
	}

	_, err = m.UnaryServerInterceptor()(
		context.Background(),
		struct{}{},
		&grpc.UnaryServerInfo{FullMethod: "/demo.Service/Call"},
		func(ctx context.Context, req any) (any, error) {
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("unary interceptor returned unexpected error: %v", err)
	}

	_, err = m.UnaryServerInterceptor()(
		context.Background(),
		struct{}{},
		&grpc.UnaryServerInfo{FullMethod: "/demo.Service/Call"},
		func(ctx context.Context, req any) (any, error) {
			return nil, status.Error(codes.Internal, "boom")
		},
	)
	if err == nil {
		t.Fatal("unary interceptor should return handler error")
	}
}

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func TestGRPCStreamInterceptor(t *testing.T) {
	m, err := NewGRPCServerMetrics(Discard(), DefaultGRPCServerMetricsConfig("svc"))
	if err != nil {
		t.Fatalf("NewGRPCServerMetrics() error = %v", err)
	}

	err = m.StreamServerInterceptor()(
		struct{}{},
		&mockServerStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/demo.Service/Stream"},
		func(srv any, stream grpc.ServerStream) error {
			return nil
		},
	)
	if err != nil {
		t.Fatalf("stream interceptor returned unexpected error: %v", err)
	}

	expectedErr := errors.New("stream failed")
	err = m.StreamServerInterceptor()(
		struct{}{},
		&mockServerStream{ctx: context.Background()},
		&grpc.StreamServerInfo{FullMethod: "/demo.Service/Stream"},
		func(srv any, stream grpc.ServerStream) error {
			return expectedErr
		},
	)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("stream interceptor error = %v, want %v", err, expectedErr)
	}
}

func TestGRPCServerMetricsObserveWithoutStatusClassLabel(t *testing.T) {
	counter := &captureCounter{}
	histogram := &captureHistogram{}
	m := &GRPCServerMetrics{
		service:      "svc",
		requestTotal: counter,
		duration:     histogram,
	}

	m.Observe(context.Background(), "/demo.Service/Call", codes.DeadlineExceeded, 20*time.Millisecond)

	if len(counter.records) != 1 {
		t.Fatalf("counter records = %d, want 1", len(counter.records))
	}

	if _, ok := labelValue(counter.records[0], LabelStatusClass); ok {
		t.Fatalf("unexpected %q label in gRPC metrics", LabelStatusClass)
	}

	code, ok := labelValue(counter.records[0], LabelGRPCCode)
	if !ok {
		t.Fatalf("missing %q label", LabelGRPCCode)
	}
	if code != "DEADLINEEXCEEDED" {
		t.Fatalf("grpc_code label = %q, want %q", code, "DEADLINEEXCEEDED")
	}
}
