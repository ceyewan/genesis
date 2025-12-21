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
├── metrics.go          # 工厂函数 New/Must/Shutdown + Meter/Counter/Gauge/Histogram 实现
├── types.go            # Counter/Gauge/Histogram/Meter 接口定义
├── config.go           # Config 结构体
├── options.go          # Option 模式 (WithLogger)
└── label.go            # Label 定义和便捷函数 L()
```

**设计原则**：

- 所有公开 API 和实现都在 `metrics` 根目录，无 `types/` 子包（完全扁平化）
- 无 `internal/metrics` 目录，避免循环依赖问题
- 用户只需导入 `metrics`，调用 `New()` 即可使用

## 核心接口

### Meter 接口

```go
// Meter 是指标的创建工厂
type Meter interface {
    // Counter 创建累加器（如：请求数、错误数）
    Counter(name string, desc string, opts ...MetricOption) (Counter, error)

    // Gauge 创建仪表盘（如：内存使用率、Goroutine 数量）
    Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)

    // Histogram 创建直方图（如：请求耗时分布）
    Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)

    // Shutdown 关闭 Meter，刷新所有指标
    Shutdown(ctx context.Context) error
}
```

### 指标类型

#### Counter 计数器

用于记录只能增加的累计值，例如 HTTP 请求数、错误次数、订单创建数等。

```go
// 创建计数器
counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")

// 增加 1
counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))

// 增加指定值
counter.Add(ctx, 5, metrics.L("endpoint", "/api/batch"))
```

#### Gauge 仪表盘

用于记录可以任意增减的瞬时值，例如内存使用率、连接数、队列长度等。

```go
// 创建仪表盘
gauge, _ := meter.Gauge("memory_usage_bytes", "内存使用字节数")

// 设置具体值
gauge.Set(ctx, 1024*1024*100, metrics.L("type", "heap"))

// 增加 1
gauge.Inc(ctx, metrics.L("node", "worker1"))

// 减少 1
gauge.Dec(ctx, metrics.L("node", "worker1"))
```

#### Histogram 直方图

用于记录值的分布情况，例如请求耗时、响应大小、延迟分布等。

```go
// 创建直方图
histogram, _ := meter.Histogram(
    "request_duration_seconds",
    "请求耗时（秒）",
    metrics.WithUnit("seconds"),
)

// 记录值
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

    // 1. 创建配置
    cfg := &metrics.Config{
        Enabled:     true,
        ServiceName: "my-service",
        Version:     "v1.0.0",
        Port:        9090,
        Path:        "/metrics",
    }

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
    Enabled     bool   `mapstructure:"enabled"`      // 是否启用指标收集
    ServiceName string `mapstructure:"service_name"` // 服务名称
    Version     string `mapstructure:"version"`      // 服务版本
    Port        int    `mapstructure:"port"`         // Prometheus 暴露端口
    Path        string `mapstructure:"path"`         // Prometheus 指标路径
}
```

### WithLogger 选项

```go
import "github.com/ceyewan/genesis/clog"

// 创建自定义 logger
logger, _ := clog.New(&clog.Config{Level: "debug"})

// 使用 WithLogger 选项
meter, err := metrics.New(cfg, metrics.WithLogger(logger))
```

## 标签使用

标签是指标的核心概念，用于实现指标的细粒度分组和筛选：

```go
// 创建标签
methodLabel := metrics.L("method", "GET")
statusLabel := metrics.L("status", "200")

// 在指标中使用
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
- 标签值相对稳定：避免高基数标签，如用户ID、请求ID等

## Prometheus 集成

当 `Enabled: true` 且设置了 `Port` 和 `Path` 时，组件会自动启动 Prometheus HTTP 服务器：

```go
cfg := &metrics.Config{
    Enabled:     true,
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

## 性能考虑

### 高基数标签

避免使用高基数的标签（如用户ID、请求ID），这会导致指标数量爆炸：

```go
// ❌ 错误：高基数标签
counter.Inc(ctx, metrics.L("user_id", "12345"))

// ✅ 正确：低基数标签
counter.Inc(ctx, metrics.L("user_type", "premium"))
```

### 并发安全

所有指标操作都是线程安全的，可以在多个 goroutine 中并发使用：

```go
// 并发使用是安全的
go func() {
    counter.Inc(ctx, metrics.L("goroutine", "worker1"))
}()

go func() {
    counter.Inc(ctx, metrics.L("goroutine", "worker2"))
}()
```

## 禁用指标

在生产环境中，可以通过设置 `Enabled: false` 来禁用指标收集：

```go
cfg := &metrics.Config{
    Enabled: false,  // 禁用指标收集
}

meter, err := metrics.New(cfg)
// 所有指标操作都是 noop，不会影响性能
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
"count"                   // 太通用
"time"                    // 缺少上下文
"metrics"                 // 重复
```

### 指标描述

提供清晰的描述，帮助用户理解指标的用途：

```go
// ✅ 良好的描述
counter, _ := meter.Counter(
    "http_requests_total",
    "HTTP 请求总数，按方法和状态码分组",
)

// ❌ 模糊的描述
counter, _ := meter.Counter(
    "http_requests_total",
    "请求计数",
)
```

### 单位使用

使用标准的单位代码：

```go
// ✅ 标准单位
metrics.WithUnit("seconds")    // 时间
metrics.WithUnit("bytes")      // 大小
metrics.WithUnit("requests")   // 计数

// ❌ 非标准单位
metrics.WithUnit("sec")        // 应该是 seconds
metrics.WithUnit("b")          // 应该是 bytes
```

## 错误处理

```go
// 基本错误处理
counter, err := meter.Counter("test_counter", "测试计数器")
if err != nil {
    log.Printf("Failed to create counter: %v", err)
    return
}

// 使用 Must 进行简单初始化（适合程序启动阶段）
meter := metrics.Must(&metrics.Config{
    Enabled: true,
    ServiceName: "my-service",
})
```

## 示例

完整的示例代码请参考 `examples/metrics/` 目录。

## 更多资源

- [OpenTelemetry Go 官方文档](https://opentelemetry.io/docs/instrumentation/go/)
- [Prometheus 最佳实践](https://prometheus.io/docs/practices/naming/)
- [Genesis 框架文档](../docs/README.md)
