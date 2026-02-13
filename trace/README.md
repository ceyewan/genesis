# trace - OpenTelemetry 链路追踪封装

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/trace.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/trace)

trace 初始化全局 TracerProvider，连接 Tempo/Jaeger 等 OTLP 后端。

## 快速开始

```go
import "github.com/ceyewan/genesis/trace"

// 初始化，返回 shutdown 函数
shutdown, err := trace.Init(&trace.Config{
    ServiceName: "my-service",
    Endpoint:    "localhost:4317",
    Sampler:     1.0,
})
defer shutdown(context.Background())
```

## API

```go
// 初始化 TracerProvider
func Init(cfg *Config) (func(context.Context) error, error)

// 创建不导出的 Provider（仅生成 TraceID）
func Discard(serviceName string) (func(context.Context) error, error)

// 默认配置
func DefaultConfig(serviceName string) *Config
```

## 通用中间件（推荐）

```go
// Gin
func GinMiddleware(serviceName string) gin.HandlerFunc

// gRPC tracing stats handler
func GRPCServerStatsHandler() stats.Handler
func GRPCClientStatsHandler() stats.Handler
```

示例：

```go
r := gin.New()
r.Use(trace.GinMiddleware("gateway"))

conn, _ := grpc.NewClient(
    "localhost:9090",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
)
```

## MQ 传播与链路关系（推荐）

组件提供统一的生产/消费 helper，消费侧可配置两种关系：

- `link`（默认）：用 Span Link 关联上游，适合异步/批处理/多消费者组
- `child_of`：用 parent/child 串成单条 Trace，适合端到端演示与排障

```go
func StartProducerSpan(
    ctx context.Context,
    tracer oteltrace.Tracer,
    spanName string,
    meta MessagingMeta,
    attrs ...attribute.KeyValue,
) (context.Context, oteltrace.Span, map[string]string)

func StartConsumerSpanFromHeaders(
    ctx context.Context,
    tracer oteltrace.Tracer,
    spanName string,
    headers map[string]string,
    meta MessagingMeta,
    attrs ...attribute.KeyValue,
) (context.Context, oteltrace.Span)

func MarkSpanError(span oteltrace.Span, err error)
```

示例：

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
        TraceRelation: trace.MessagingTraceRelationChildOf, // 或 MessagingTraceRelationLink
    },
)
defer consumeSpan.End()
```

## 语义契约

- 消息属性键：`messaging.system`、`messaging.destination`、`messaging.operation`、`messaging.consumer.group`
- 常用系统：`nats`
- span 命名 helper：`SpanNameMQPublish`、`SpanNameMQConsume`

## 使用

```go
tracer := otel.Tracer("my-component")
ctx, span := tracer.Start(ctx, "operation")
defer span.End()

// 添加属性
span.SetAttributes(attribute.String("key", "value"))
```

## License

[MIT License](../../LICENSE)
