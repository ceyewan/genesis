# metrics - OpenTelemetry 指标收集

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/metrics.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/metrics)

基于 OpenTelemetry 的指标收集，内置 Prometheus HTTP 服务器。

## 快速开始

```go
import "github.com/ceyewan/genesis/metrics"

meter, err := metrics.New(&metrics.Config{
    ServiceName: "my-service",
    Version:     "v1.0.0",
    Port:        9090,
    Path:        "/metrics",
})
defer meter.Shutdown(ctx)

counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
```

## API

```go
// 创建 Meter
func New(cfg *Config) (Meter, error)

// 空操作 Meter
func Discard() Meter

// 默认配置
func NewDevDefaultConfig(serviceName string) *Config
func NewProdDefaultConfig(serviceName, version string) *Config
```

## 通用服务端埋点（推荐）

组件内置了可复用的 HTTP/gRPC 服务端 RED 指标封装，避免在业务侧重复实现。

```go
// HTTP
func DefaultHTTPServerMetricsConfig(service string) *HTTPServerMetricsConfig
func NewHTTPServerMetrics(m Meter, cfg *HTTPServerMetricsConfig) (*HTTPServerMetrics, error)
func (m *HTTPServerMetrics) Observe(ctx context.Context, method, route string, status int, duration time.Duration)
func GinHTTPMiddleware(httpMetrics *HTTPServerMetrics) gin.HandlerFunc

// gRPC
func DefaultGRPCServerMetricsConfig(service string) *GRPCServerMetricsConfig
func NewGRPCServerMetrics(m Meter, cfg *GRPCServerMetricsConfig) (*GRPCServerMetrics, error)
func (m *GRPCServerMetrics) Observe(ctx context.Context, fullMethod string, code codes.Code, duration time.Duration)
func (m *GRPCServerMetrics) UnaryServerInterceptor() grpc.UnaryServerInterceptor
func (m *GRPCServerMetrics) StreamServerInterceptor() grpc.StreamServerInterceptor
```

### Gin 示例

```go
httpCfg := metrics.DefaultHTTPServerMetricsConfig("gateway")
httpCfg.RequestDurationName = "http_request_duration_seconds" // 兼容现有仪表盘命名
httpMetrics, _ := metrics.NewHTTPServerMetrics(meter, httpCfg)

r := gin.New()
r.Use(gin.Recovery())
r.Use(metrics.GinHTTPMiddleware(httpMetrics))
```

`GinHTTPMiddleware` 在未命中路由时会把 `route` 标签收敛为 `unknown`，避免将原始 URL Path 写入标签导致高基数问题。

### gRPC 示例

```go
grpcMetrics, _ := metrics.NewGRPCServerMetrics(
    meter,
    metrics.DefaultGRPCServerMetricsConfig("logic"),
)

srv := grpc.NewServer(
    grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
)
```

## 指标契约（推荐统一）

为降低跨服务看板与告警维护成本，建议统一使用以下标签和值约定：

- HTTP 标签：`service`、`operation`、`method`、`route`、`status_class`、`outcome`
- gRPC 标签：`service`、`operation`、`method`、`route`、`grpc_code`、`outcome`
- 操作：`http.server`、`grpc.server`
- 结果：`success`、`error`

## 指标类型

```go
// Counter - 计数器（只增）
counter.Inc(ctx, labels...)
counter.Add(ctx, value, labels...)

// Gauge - 仪表盘（可增减）
gauge.Set(ctx, value, labels...)
gauge.Inc(ctx, labels...)
gauge.Dec(ctx, labels...)

// Histogram - 直方图（分布）
histogram.Record(ctx, value, labels...)
```

## 标签

```go
metrics.L("key", "value")
```

避免高基数标签（如 user_id、request_id）。

## Prometheus 配置

```yaml
scrape_configs:
  - job_name: "my-service"
    static_configs:
      - targets: ["localhost:9090"]
```

## License

[MIT License](../../LICENSE)
