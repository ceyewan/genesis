package metrics

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/ceyewan/genesis/xerrors"
)

const (
	MetricHTTPServerRequestTotal    = "http_server_requests_total"
	MetricHTTPServerDurationSeconds = "http_server_request_duration_seconds"
)

var defaultHTTPDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// HTTPServerMetricsConfig 配置可重用的 HTTP 服务器指标
type HTTPServerMetricsConfig struct {
	Service             string
	RequestTotalName    string
	RequestDurationName string
	DurationBuckets     []float64
	StaticLabels        []Label
}

// DefaultHTTPServerMetricsConfig 返回默认的 HTTP 服务器指标配置
func DefaultHTTPServerMetricsConfig(service string) *HTTPServerMetricsConfig {
	return &HTTPServerMetricsConfig{
		Service:             service,
		RequestTotalName:    MetricHTTPServerRequestTotal,
		RequestDurationName: MetricHTTPServerDurationSeconds,
		DurationBuckets:     defaultHTTPDurationBuckets,
	}
}

// HTTPServerMetrics 封装可重用的 HTTP 服务器 RED 指标集
type HTTPServerMetrics struct {
	service      string
	requestTotal Counter
	duration     Histogram
	staticLabels []Label
}

// NewHTTPServerMetrics 创建可重用的 HTTP 服务器指标
func NewHTTPServerMetrics(m Meter, cfg *HTTPServerMetricsConfig) (*HTTPServerMetrics, error) {
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
		requestTotalName = MetricHTTPServerRequestTotal
	}

	requestDurationName := strings.TrimSpace(cfg.RequestDurationName)
	if requestDurationName == "" {
		requestDurationName = MetricHTTPServerDurationSeconds
	}

	counter, err := m.Counter(requestTotalName, "Total number of HTTP requests.")
	if err != nil {
		return nil, xerrors.Wrap(err, "create http request counter")
	}

	histogramOpts := []MetricOption{WithUnit("s")}
	if len(cfg.DurationBuckets) > 0 {
		histogramOpts = append(histogramOpts, WithBuckets(cfg.DurationBuckets))
	}
	duration, err := m.Histogram(requestDurationName, "HTTP request duration in seconds.", histogramOpts...)
	if err != nil {
		return nil, xerrors.Wrap(err, "create http request duration histogram")
	}

	static := make([]Label, len(cfg.StaticLabels))
	copy(static, cfg.StaticLabels)

	return &HTTPServerMetrics{
		service:      service,
		requestTotal: counter,
		duration:     duration,
		staticLabels: static,
	}, nil
}

// Observe 记录 HTTP 请求 RED 指标
func (m *HTTPServerMetrics) Observe(ctx context.Context, method string, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}

	safeMethod := strings.ToUpper(strings.TrimSpace(method))
	if safeMethod == "" {
		safeMethod = http.MethodGet
	}

	safeRoute := strings.TrimSpace(route)
	if safeRoute == "" {
		safeRoute = UnknownRoute
	}

	labels := make([]Label, 0, len(m.staticLabels)+6)
	labels = append(labels, m.staticLabels...)
	labels = append(labels,
		L(LabelService, m.service),
		L(LabelOperation, OperationHTTPServer),
		L(LabelMethod, safeMethod),
		L(LabelRoute, safeRoute),
		L(LabelStatusClass, HTTPStatusClass(status)),
		L(LabelOutcome, HTTPOutcome(status)),
	)

	m.requestTotal.Inc(ctx, labels...)
	m.duration.Record(ctx, duration.Seconds(), labels...)
}
