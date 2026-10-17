# metrics

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/metrics.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/metrics)

`metrics` 是 Genesis 的 L0 指标组件，基于 OpenTelemetry 提供统一的指标创建能力，并可选暴露 Prometheus HTTP 端点。它面向微服务和组件库场景，解决统一指标接口、Prometheus 暴露和 HTTP/gRPC 服务端 RED 埋点的复用问题。

## 组件定位

`metrics` 当前采用**全局模式**工作：

- `New()` 会创建 `MeterProvider`
- 同时会把它安装为 OpenTelemetry 全局 `MeterProvider`
- 仓库内依赖全局 provider 的埋点库会立即共享这套状态

这意味着它更适合作为**应用启动时初始化一次**的基础组件，而不是在运行时频繁创建多个实例。

## 快速开始

```go
logger, err := clog.New(clog.NewProdDefaultConfig(projectRoot))
if err != nil {
    return err
}
defer logger.Close()

meter, err := metrics.New(&metrics.Config{
    ServiceName:   "my-service",
    Version:       "v1.0.0",
	ListenAddress: "127.0.0.1",
    Port:          9090,
    Path:          "/metrics",
}, metrics.WithLogger(logger))
if err != nil {
    return err
}
defer meter.Shutdown(ctx)

counter, err := meter.Counter("http_requests_total", "HTTP 请求总数")
if err != nil {
    return err
}
counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
```

## 配置约定

`Config` 的关键行为：

- `ServiceName` 必填
- `Version`、`InstanceID`、`Environment` 对应统一资源字段 `service.version`、`service.instance.id`、`deployment.environment.name`
- `Port > 0` 且 `Path` 非空时，组件会启动 Prometheus HTTP 端点
- `ListenAddress` 控制监听网卡；开发环境建议 `127.0.0.1`，生产环境按部署网络显式配置
- `Port == 0` 且 `Path` 为空时不启动 HTTP 服务；只配置其中一个会返回配置错误
- `CardinalityLimit` 限制每个 Instrument 每次采集的标签组合数，包含溢出系列；0 使用默认 2000，负值非法。
  超限数据由 OTel 归入 `otel.metric.overflow=true`，保留有限系列而丢失超限标签维度；Gauge 溢出系列只保留最近记录，不表示各标签值之和。

当前若 metrics HTTP 端口监听失败，`New()` 会直接返回错误，而不是在后台异步失败。
`WithLogger` 可注入统一的应用 Logger；未注入时 metrics 内部日志静默丢弃，且组件不会关闭注入的 Logger。

## 服务端埋点

组件内置了可复用的 HTTP/gRPC 服务端 RED 指标封装，避免业务侧重复实现。

### Gin

```go
httpMetrics, _ := metrics.NewHTTPServerMetrics(
    meter,
    metrics.DefaultHTTPServerMetricsConfig("gateway"),
)

r := gin.New()
r.Use(metrics.GinHTTPMiddleware(httpMetrics))
```

`GinHTTPMiddleware` 在未命中路由时会把 `route` 收敛为 `unknown`，避免把原始 URL path 写入标签导致高基数。

### gRPC

```go
grpcMetrics, _ := metrics.NewGRPCServerMetrics(
    meter,
    metrics.DefaultGRPCServerMetricsConfig("logic"),
)

srv := grpc.NewServer(
    grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
)
```

## Gauge 与增减计数

`Gauge.Set` 记录调用方计算的当前值，不维护每组标签的本地累计状态。
需要累计正负增量时，使用原生 `UpDownCounter`，不要把 Gauge 当作计数器：

```go
inflight, err := meter.UpDownCounter("requests_inflight", "正在处理的请求数")
if err != nil {
    return err
}
inflight.Add(ctx, 1)
defer inflight.Add(ctx, -1)
```

`UpDownCounter` 没有 `Set`；新建或重新初始化会重置其累计状态。不要用请求 ID、用户 ID 等无界标签。

## 生命周期

- `New()` 通常应在应用启动时调用一次
- `Shutdown()` 负责关闭 HTTP 服务和底层 `MeterProvider`
- 如果当前全局 `MeterProvider` 仍指向该实例，`Shutdown()` 还会把全局状态重置为 no-op provider
- `Shutdown(ctx)` 可并发重复调用，并遵守调用方 deadline

## 推荐实践

- 生产环境统一使用一个全局 `metrics` 实例
- 业务指标优先通过 `Counter` / `UpDownCounter` / `Histogram` / `Gauge` 统一创建，不要直接散落使用底层 OTel API
- 避免高基数标签，例如 `user_id`、`request_id`
- 使用内置的 HTTP/gRPC 服务端埋点封装，统一标签结构与命名

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/metrics)
- [可观测性设计取舍](../docs/design/observability.md)
- [Genesis 文档目录](../docs/README.md)
