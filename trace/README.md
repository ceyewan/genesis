# trace

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/trace.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/trace)

`trace` 是 Genesis 的 L0 链路追踪组件，基于 OpenTelemetry 提供全局 tracing 初始化、Gin/gRPC 中间件包装，以及 MQ 场景下的传播辅助函数。它面向微服务和组件库场景，解决 tracing 初始化、上下文传播和消息消费关系建模的统一问题。

## 组件定位

`trace` 当前采用**全局模式**工作：

- `Init()` 会创建 `TracerProvider`
- 同时会把它安装为 OpenTelemetry 全局 `TracerProvider`
- 也会安装全局 `TextMapPropagator`

这意味着它更适合作为**应用启动时初始化一次**的基础组件，而不是在运行时反复调用。

`Discard()` 也是全局模式：它不是一个局部 helper，而是安装一个“不导出数据”的全局 provider，仅在本地生成 TraceID 并维持传播链路。

## 快速开始

```go
shutdown, err := trace.Init(&trace.Config{
    ServiceName: "my-service",
    Endpoint:    "localhost:4317",
    Sampler:     1.0,
})
if err != nil {
    return err
}
defer shutdown(context.Background())
```

## 配置边界

`trace.Config` 当前是一个**最小 OTLP gRPC 初始化器**，支持：

- `ServiceName`
- `Endpoint`
- `Sampler`
- `Batcher`，可选 `batch` 或 `simple`
- `Insecure`

其中 `Batcher` 在默认配置里会设置为 `batch`，而空字符串行为也等同于 `batch`，适合常规服务；`simple` 更适合测试或需要更直接刷出的场景。组件当前不负责更复杂的 exporter 能力，例如 TLS、认证头和附加 resource attributes。

## HTTP / gRPC 中间件

```go
r := gin.New()
r.Use(trace.GinMiddleware("gateway"))

conn, _ := grpc.NewClient(
    "localhost:9090",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
)
```

## MQ 传播与链路关系

组件提供统一的生产/消费 helper，消费侧支持两种关系：

- `link`：默认值，适合异步、批处理、多消费者组
- `child_of`：适合端到端演示和串成单条 trace 的排障场景

```go
pubCtx, pubSpan, headers := trace.StartProducerSpan(
    ctx,
    tracer,
    trace.SpanNameMQPublish("orders.created"),
    trace.MessagingMeta{
        System:      trace.MessagingSystemNATS,
        Destination: "orders.created",
        Operation:   trace.MessagingOperationPublish,
    },
)
defer pubSpan.End()

consumeCtx, consumeSpan := trace.StartConsumerSpanFromHeaders(
    msg.Context(),
    tracer,
    trace.SpanNameMQConsume("orders.created"),
    msg.Headers(),
    trace.MessagingMeta{
        System:        trace.MessagingSystemNATS,
        Destination:   "orders.created",
        Operation:     trace.MessagingOperationProcess,
        ConsumerGroup: "workers",
        TraceRelation: trace.MessagingTraceRelationLink,
    },
)
defer consumeSpan.End()
```

## 生命周期

- `Init()` 通常应在应用启动时调用一次
- 返回的 `shutdown` 函数由调用方负责执行；关闭后若全局状态仍指向该实例，会回退到安全默认值
- `Discard()` 虽然不导出 trace 数据，但仍然会修改全局 tracing 状态

## 推荐实践

- 应用启动时统一初始化一次 `trace`
- HTTP、gRPC、MQ 传播都共用同一套全局 tracing 状态
- 异步消息默认使用 `link`，只在确实需要单条 trace 演示时使用 `child_of`
- 不要把 `Discard()` 当成“无副作用的局部 tracer helper”

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/trace)
- [可观测性实践](../docs/genesis-observability-blog.md)
- [Genesis 文档目录](../docs/README.md)
