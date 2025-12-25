[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/metrics.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/metrics)

# metrics - Genesis 统一指标收集组件

`metrics` 是 Genesis 框架的统一指标收集组件，基于 OpenTelemetry 标准构建。

## 特性

- **所属层级**：L0 (Base) — 框架基石，为所有组件提供可观测性
- **核心职责**：提供统一的指标收集、上报和暴露接口
- **设计原则**：
    - 基于 OpenTelemetry 标准，确保与云原生生态兼容
    - 极简接口，屏蔽 OTel SDK 的复杂性
    - 与 Connector/组件深度集成，自动埋点

> **注意**：Genesis 当前只提供 Metrics 能力。如需 Tracing（链路追踪），请直接使用 OpenTelemetry SDK。

## 目录结构（完全扁平化设计）

```text
metrics/                 # 公开 API + 实现（完全扁平化）
├── metrics.go          # 工厂函数 New/Discard + Meter/Counter/Gauge/Histogram 实现
├── types.go            # Counter/Gauge/Histogram/Meter 接口定义
├── config.go           # Config 结构体 + 默认配置工厂
├── options.go          # Option 模式 (WithLogger)
└── label.go            # Label 定义和便捷函数 L()
```

## 核心接口

### Meter 接口

```go
type Meter interface {
    Counter(name string, desc string, opts ...MetricOption) (Counter, error)
    Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)
    Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)
    Shutdown(ctx context.Context) error
}
```

### 指标类型

#### Counter 计数器

用于记录只能增加的累计值，例如 HTTP 请求数、错误次数、订单创建数等。

```go
counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")

counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
counter.Add(ctx, 5, metrics.L("endpoint", "/api/batch"))
```

#### Gauge 仪表盘

用于记录可以任意增减的瞬时值，例如内存使用率、连接数、队列长度等。

```go
gauge, _ := meter.Gauge("memory_usage_bytes", "内存使用字节数")

gauge.Set(ctx, 1024*1024*100, metrics.L("type", "heap"))
gauge.Inc(ctx, metrics.L("node", "worker1"))
gauge.Dec(ctx, metrics.L("node", "worker1"))
```

#### Histogram 直方图

用于记录值的分布情况，例如请求耗时、响应大小、延迟分布等。

```go
histogram, _ := meter.Histogram(
    "request_duration_seconds",
    "请求耗时（秒）",
    metrics.WithUnit("seconds"),
)

histogram.Record(ctx, 0.123, metrics.L("endpoint", "/api/users"))
```

## 快速开始

### 基本使用

```go
package main

import (
    "context"
    "log"

    "github.com/ceyewan/genesis/metrics"
)

func main() {
    ctx := context.Background()

    // 1. 创建配置（使用默认配置工厂）
    cfg := metrics.NewDevDefaultConfig("my-service")

    // 2. 创建 Meter
    meter, err := metrics.New(cfg)
    if err != nil {
        log.Fatal(err)
    }
    defer meter.Shutdown(ctx)

    // 3. 创建指标
    requestCounter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
    responseTime, _ := meter.Histogram(
        "response_time_seconds",
        "响应时间",
        metrics.WithUnit("seconds"),
    )

    // 4. 记录指标
    requestCounter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
    responseTime.Record(ctx, 0.123, metrics.L("endpoint", "/api/users"))
}
```

### 配置项说明

```go
type Config struct {
    ServiceName string `mapstructure:"service_name"` // 服务名称
    Version     string `mapstructure:"version"`      // 服务版本
    Port        int    `mapstructure:"port"`         // Prometheus 暴露端口
    Path        string `mapstructure:"path"`         // Prometheus 指标路径
}
```

### 默认配置工厂

```go
// 开发环境
cfg := metrics.NewDevDefaultConfig("my-service")
// 等价于：&metrics.Config{ServiceName: "my-service", Version: "dev", Port: 9090, Path: "/metrics"}

// 生产环境
cfg := metrics.NewProdDefaultConfig("my-service", "v1.2.3")
// 等价于：&metrics.Config{ServiceName: "my-service", Version: "v1.2.3", Port: 9090, Path: "/metrics"}
```

### WithLogger 选项

```go
import "github.com/ceyewan/genesis/clog"

logger, _ := clog.New(&clog.Config{Level: "debug"})
meter, err := metrics.New(cfg, metrics.WithLogger(logger))
```

## 禁用指标

使用 `Discard()` 创建 noop Meter：

```go
meter := metrics.Discard()
// 所有指标操作都是 noop，不会影响性能
```

## 标签使用

标签用于实现指标的细粒度分组和筛选：

```go
// 创建标签
methodLabel := metrics.L("method", "GET")
statusLabel := metrics.L("status", "200")

counter.Inc(ctx, methodLabel, statusLabel)

// 也可以直接创建多个标签
counter.Inc(ctx,
    metrics.L("method", "POST"),
    metrics.L("endpoint", "/api/users"),
    metrics.L("status", "201"),
)
```

### 标签命名规范

- 使用小写字母和下划线：`user_id` 而不是 `userId`
- 避免使用保留字：避免使用 "id"、"name" 等通用词汇
- 控制标签数量：每个指标的标签数量不宜过多（建议 < 10个）
- **避免高基数标签**：如用户ID、请求ID等

## Prometheus 集成

设置了 `Port` 和 `Path` 时，组件会自动启动 Prometheus HTTP 服务器：

```go
cfg := &metrics.Config{
    ServiceName: "user-service",
    Version:     "v1.0.0",
    Port:        9090,
    Path:        "/metrics",
}

meter, err := metrics.New(cfg)
// Prometheus 指标将在 http://localhost:9090/metrics 暴露
```

### Prometheus 配置

在 `prometheus.yml` 中添加抓取配置：

```yaml
scrape_configs:
    - job_name: "genesis-services"
      static_configs:
          - targets: ["localhost:9090"]
      scrape_interval: 15s
      metrics_path: /metrics
```

## 最佳实践

### 指标命名

遵循 Prometheus 命名规范：

```go
// ✅ 良好的命名
"http_requests_total"      // 总数指标以 _total 结尾
"request_duration_seconds" // 时间单位作为后缀
"memory_usage_bytes"       // 单位作为后缀

// ❌ 避免的命名
"count"   // 太通用
"time"    // 缺少上下文
"metrics" // 重复
```

### 避免高基数标签

```go
// ❌ 错误：高基数标签
counter.Inc(ctx, metrics.L("user_id", "12345"))

// ✅ 正确：低基数标签
counter.Inc(ctx, metrics.L("user_type", "premium"))
```

## 示例

完整的示例代码请参考 `examples/metrics/` 目录。

## 更多资源

- [OpenTelemetry Go 官方文档](https://opentelemetry.io/docs/instrumentation/go/)
- [Prometheus 最佳实践](https://prometheus.io/docs/practices/naming/)
- [Genesis 框架文档](../docs/README.md)
