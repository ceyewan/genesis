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
meter, err := metrics.New(&metrics.Config{
    ServiceName: "my-service",
    Version:     "v1.0.0",
    Port:        9090,
    Path:        "/metrics",
})
if err != nil {
    return err
}
defer meter.Shutdown(ctx)

counter, _ := meter.Counter("http_requests_total", "HTTP 请求总数")
counter.Inc(ctx, metrics.L("method", "GET"), metrics.L("status", "200"))
```

## 配置约定

`Config` 的关键行为有三点：

- `ServiceName` 必填
- `Port > 0` 且 `Path` 非空时，组件会启动 Prometheus HTTP 端点
- `Port == 0` 时不启动 HTTP 服务，只保留进程内指标能力

当前若 metrics HTTP 端口监听失败，`New()` 会直接返回错误，而不是在后台异步失败。

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

## 生命周期

- `New()` 通常应在应用启动时调用一次
- `Shutdown()` 负责关闭 HTTP 服务和底层 `MeterProvider`
- 如果当前全局 `MeterProvider` 仍指向该实例，`Shutdown()` 还会把全局状态重置为 no-op provider
- `Shutdown()` 当前不是幂等承诺接口，推荐按“谁创建，谁关闭”原则调用一次

## 推荐实践

- 生产环境统一使用一个全局 `metrics` 实例
- 业务指标优先通过 `Counter` / `Histogram` / `Gauge` 统一创建，不要直接散落使用底层 OTel API
- 避免高基数标签，例如 `user_id`、`request_id`
- 使用内置的 HTTP/gRPC 服务端埋点封装，统一标签结构与命名

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/metrics)
- [可观测性实践](../docs/genesis-observability-blog.md)
- [Genesis 文档目录](../docs/README.md)
