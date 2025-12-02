# Metrics 设计文档

## 1. 概述

Metrics 组件为 Genesis 框架提供统一的指标收集能力，基于 OpenTelemetry 标准构建。

* **所属层级**：L0 (Base) — 框架基石，为所有组件提供可观测性
* **核心职责**：提供统一的指标收集、上报和暴露接口
* **设计原则**：
  * 基于 OpenTelemetry 标准，确保与云原生生态兼容
  * 极简接口，屏蔽 OTel SDK 的复杂性
  * 与 Connector/组件深度集成，自动埋点

> **注意**：Genesis 当前只提供 Metrics 能力。如需 Tracing（链路追踪），请直接使用 OpenTelemetry SDK。

## 2. 目录结构

```text
pkg/metrics/
├── metrics.go       # 工厂函数 + Meter 接口 + Config
├── counter.go       # Counter 接口与实现
├── gauge.go         # Gauge 接口与实现
├── histogram.go     # Histogram 接口与实现
├── options.go       # Option 模式定义
└── label.go         # Label 定义

internal/metrics/
├── provider.go      # OTel Provider 初始化
├── prometheus.go    # Prometheus Exporter
└── otlp.go          # OTLP Exporter
```

## 3. 核心接口

### 3.1 Meter 接口

```go
// Meter 是指标的创建工厂
type Meter interface {
    // Counter 创建累加器（如：请求数、错误数）
    Counter(name string, desc string, opts ...MetricOption) (Counter, error)
    
    // Gauge 创建仪表盘（如：内存使用率、Goroutine 数量）
    Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)
    
    // Histogram 创建直方图（如：请求耗时分布）
    Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)
}
```

### 3.2 指标类型

```go
// Counter 累加器
type Counter interface {
    Inc(ctx context.Context, labels ...Label)
    Add(ctx context.Context, val float64, labels ...Label)
}

// Gauge 仪表盘
type Gauge interface {
    Set(ctx context.Context, val float64, labels ...Label)
    Inc(ctx context.Context, labels ...Label)
    Dec(ctx context.Context, labels ...Label)
}

// Histogram 直方图
type Histogram interface {
    Record(ctx context.Context, val float64, labels ...Label)
}
```

### 3.3 Label 定义

```go
// Label 定义指标的维度
type Label struct {
    Key   string
    Value string
}

// 便捷构造函数
func L(key, value string) Label {
    return Label{Key: key, Value: value}
}
```

### 3.4 Config 结构体

```go
// Config 指标配置
type Config struct {
    Enabled     bool   `mapstructure:"enabled"`
    ServiceName string `mapstructure:"service_name"`
    Version     string `mapstructure:"version"`
    
    // Prometheus Exporter
    Port int    `mapstructure:"port"` // 默认 9090
    Path string `mapstructure:"path"` // 默认 /metrics
    
    // OTLP Exporter（可选）
    OTLPEndpoint string `mapstructure:"otlp_endpoint"`
    OTLPProtocol string `mapstructure:"otlp_protocol"` // grpc | http
}
```

## 4. 工厂函数

```go
// New 创建 Meter 实例
func New(cfg *Config, opts ...Option) (Meter, error)

// Must 类似 New，但出错时 panic
func Must(cfg *Config, opts ...Option) Meter

// Shutdown 关闭 Meter，刷新所有指标
func (m *meter) Shutdown(ctx context.Context) error
```

## 5. Option 模式

```go
type options struct {
    logger clog.Logger
}

type Option func(*options)

func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("metrics")
    }
}
```

## 6. 配置示例

```yaml
# config.yaml
metrics:
  enabled: true
  service_name: order-service
  version: v1.0.0
  port: 9090
  path: /metrics
  
  # 可选：OTLP Exporter
  # otlp_endpoint: otel-collector:4317
  # otlp_protocol: grpc
```

## 7. 与其他组件的集成

### 7.1 Connector 集成

所有 Connector 通过 `WithMeter` 注入 Meter：

```go
// pkg/connector/options.go
func WithMeter(m metrics.Meter) Option {
    return func(o *options) {
        o.meter = m
    }
}

// 在 Connector 内部自动埋点
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
    // ...
    if opt.meter != nil {
        c.poolActive, _ = opt.meter.Gauge("redis_pool_active", "Active connections")
        c.poolIdle, _ = opt.meter.Gauge("redis_pool_idle", "Idle connections")
        c.cmdDuration, _ = opt.meter.Histogram("redis_cmd_duration_seconds", "Command duration")
    }
}
```

### 7.2 组件集成

所有 L2/L3 组件遵循相同模式：

```go
// pkg/dlock/options.go
func WithMeter(m metrics.Meter) Option {
    return func(o *options) {
        o.meter = m
    }
}

// 在组件内部自动埋点
func NewRedis(conn connector.RedisConnector, cfg *Config, opts ...Option) (Locker, error) {
    // ...
    if opt.meter != nil {
        l.acquireTotal, _ = opt.meter.Counter("dlock_acquire_total", "Lock acquire attempts")
        l.acquireDuration, _ = opt.meter.Histogram("dlock_acquire_duration_seconds", "Lock acquire duration")
    }
}
```

### 7.3 典型使用流程

```go
func main() {
    ctx := context.Background()

    // 1. 加载配置
    cfg := config.MustLoad("config.yaml")

    // 2. 初始化 Logger
    logger := clog.Must(&cfg.Log)

    // 3. 初始化 Metrics
    meter, _ := metrics.New(&cfg.Metrics,
        metrics.WithLogger(logger),
    )
    defer meter.Shutdown(ctx)

    // 4. 创建 Connectors（注入 Logger + Meter）
    redisConn, _ := connector.NewRedis(&cfg.Redis,
        connector.WithLogger(logger),
        connector.WithMeter(meter),
    )
    defer redisConn.Close()

    // 5. 创建组件（注入 Logger + Meter）
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock,
        dlock.WithLogger(logger),
        dlock.WithMeter(meter),
    )

    // 6. 业务逻辑中使用自定义指标
    reqCounter, _ := meter.Counter("http_requests_total", "Total HTTP requests")
    reqCounter.Inc(ctx, metrics.L("method", "POST"), metrics.L("path", "/users"))
}
```

## 8. 内置指标

### 8.1 Connector 指标

| 指标名 | 类型 | 描述 |
|--------|------|------|
| `connector_pool_active` | Gauge | 活跃连接数 |
| `connector_pool_idle` | Gauge | 空闲连接数 |
| `connector_pool_wait` | Gauge | 等待连接数 |
| `connector_errors_total` | Counter | 错误计数 |
| `connector_latency_seconds` | Histogram | 操作延迟 |

**Label 维度**：`type` (redis/mysql/etcd), `name` (连接器名称)

### 8.2 组件指标

| 组件 | 指标名 | 类型 | 描述 |
|------|--------|------|------|
| dlock | `dlock_acquire_total` | Counter | 锁获取次数 |
| dlock | `dlock_acquire_duration_seconds` | Histogram | 锁获取耗时 |
| cache | `cache_hits_total` | Counter | 缓存命中 |
| cache | `cache_misses_total` | Counter | 缓存未命中 |
| ratelimit | `ratelimit_allowed_total` | Counter | 允许请求数 |
| ratelimit | `ratelimit_denied_total` | Counter | 拒绝请求数 |

## 9. 使用示例

### 9.1 自定义 Counter

```go
reqCounter, _ := meter.Counter("http_requests_total", "Total HTTP requests")

func HandleRequest(ctx context.Context, method, path string, status int) {
    reqCounter.Inc(ctx,
        metrics.L("method", method),
        metrics.L("path", path),
        metrics.L("status", strconv.Itoa(status)),
    )
}
```

### 9.2 自定义 Histogram

```go
latency, _ := meter.Histogram("http_request_duration_seconds", "Request duration")

func HandleRequest(ctx context.Context) {
    start := time.Now()
    defer func() {
        latency.Record(ctx, time.Since(start).Seconds(),
            metrics.L("method", "GET"),
            metrics.L("path", "/users"),
        )
    }()
    
    // 业务逻辑...
}
```

### 9.3 自定义 Gauge

```go
activeConns, _ := meter.Gauge("active_connections", "Active connections")

// 在连接池回调中更新
activeConns.Set(ctx, float64(pool.ActiveCount()))
```

## 10. Prometheus 集成

启用 Prometheus Exporter 后，访问 `http://localhost:9090/metrics` 获取指标：

```text
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="POST",path="/users",status="200"} 42

# HELP redis_pool_active Active connections
# TYPE redis_pool_active gauge
redis_pool_active{type="redis",name="default"} 5
```

## 11. 技术实现

* **底层实现**：基于 OpenTelemetry Go SDK
* **Exporter**：默认 Prometheus，可选 OTLP
* **线程安全**：所有指标操作并发安全
