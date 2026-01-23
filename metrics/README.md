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
