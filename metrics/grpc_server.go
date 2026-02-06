package metrics

import (
	"context"
	"strings"
	"time"

	"github.com/ceyewan/genesis/xerrors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	MetricGRPCServerRequestTotal    = "grpc_server_requests_total"
	MetricGRPCServerDurationSeconds = "grpc_server_request_duration_seconds"
)

var defaultGRPCDurationBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// GRPCServerMetricsConfig 配置可重用的 gRPC 服务器指标
type GRPCServerMetricsConfig struct {
	Service             string
	RequestTotalName    string
	RequestDurationName string
	DurationBuckets     []float64
	StaticLabels        []Label
}

// DefaultGRPCServerMetricsConfig 返回默认的 gRPC 服务器指标配置
func DefaultGRPCServerMetricsConfig(service string) *GRPCServerMetricsConfig {
	return &GRPCServerMetricsConfig{
		Service:             service,
		RequestTotalName:    MetricGRPCServerRequestTotal,
		RequestDurationName: MetricGRPCServerDurationSeconds,
		DurationBuckets:     defaultGRPCDurationBuckets,
	}
}

// GRPCServerMetrics 封装可重用的 gRPC 服务器 RED 指标集
type GRPCServerMetrics struct {
	service      string
	requestTotal Counter
	duration     Histogram
	staticLabels []Label
}

// NewGRPCServerMetrics 创建可重用的 gRPC 服务器指标
func NewGRPCServerMetrics(m Meter, cfg *GRPCServerMetricsConfig) (*GRPCServerMetrics, error) {
	if m == nil {
		return nil, xerrors.New("meter is nil")
	}
	if cfg == nil {
		return nil, xerrors.New("config is nil")
	}

	service := strings.TrimSpace(cfg.Service)
	if service == "" {
		service = "unknown"
	}

	requestTotalName := strings.TrimSpace(cfg.RequestTotalName)
	if requestTotalName == "" {
		requestTotalName = MetricGRPCServerRequestTotal
	}
	requestDurationName := strings.TrimSpace(cfg.RequestDurationName)
	if requestDurationName == "" {
		requestDurationName = MetricGRPCServerDurationSeconds
	}

	counter, err := m.Counter(requestTotalName, "Total number of gRPC requests.")
	if err != nil {
		return nil, xerrors.Wrap(err, "create grpc request counter")
	}

	histogramOpts := []MetricOption{WithUnit("s")}
	if len(cfg.DurationBuckets) > 0 {
		histogramOpts = append(histogramOpts, WithBuckets(cfg.DurationBuckets))
	}
	duration, err := m.Histogram(requestDurationName, "gRPC request duration in seconds.", histogramOpts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "create grpc request duration histogram")
	}

	static := make([]Label, len(cfg.StaticLabels))
	copy(static, cfg.StaticLabels)

	return &GRPCServerMetrics{
		service:      service,
		requestTotal: counter,
		duration:     duration,
		staticLabels: static,
	}, nil
}

// Observe 记录 gRPC RED 指标
func (m *GRPCServerMetrics) Observe(ctx context.Context, fullMethod string, code codes.Code, duration time.Duration) {
	if m == nil {
		return
	}

	method := strings.TrimSpace(fullMethod)
	if method == "" {
		method = "unknown"
	}

	codeStr := strings.ToUpper(code.String())
	if codeStr == "" {
		codeStr = "UNKNOWN"
	}

	labels := make([]Label, 0, len(m.staticLabels)+6)
	labels = append(labels, m.staticLabels...)
	labels = append(labels,
		L(LabelService, m.service),
		L(LabelOperation, OperationGRPCServer),
		L(LabelMethod, method),
		L(LabelRoute, method),
		L(LabelGRPCCode, codeStr),
		L(LabelOutcome, GRPCOutcome(code)),
	)

	m.requestTotal.Inc(ctx, labels...)
	m.duration.Record(ctx, duration.Seconds(), labels...)
}

// UnaryServerInterceptor 返回一个可重用的 grpc.UnaryServerInterceptor
func (m *GRPCServerMetrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		m.Observe(ctx, info.FullMethod, status.Code(err), time.Since(start))
		return resp, err
	}
}

// StreamServerInterceptor 返回一个可重用的 grpc.StreamServerInterceptor
func (m *GRPCServerMetrics) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()
		err := handler(srv, ss)
		ctx := context.Background()
		if ss != nil {
			ctx = ss.Context()
		}
		m.Observe(ctx, info.FullMethod, status.Code(err), time.Since(start))
		return err
	}
}
