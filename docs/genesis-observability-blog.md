# Genesis Observability：基于 OpenTelemetry 的可观测性实践

在微服务架构中，服务数量的爆炸式增长使得传统的监控和调试手段捉襟见肘。"请求在哪里失败了？"、"为什么这个 API 响应突然变慢？"、"各个服务之间的调用关系是怎样的？" 这些问题如果不具备完善的可观测性体系，将成为开发者的噩梦。

Genesis 提供了一套基于 OpenTelemetry 标准的开箱即用的可观测性解决方案，将 **Metrics（指标）**、**Logging（日志）** 和 **Tracing（链路追踪）** 三大支柱无缝集成，特别是结合 **LGTM** (Loki, Grafana, Tempo, Mimir/Prometheus) 技术栈，为开发者提供上帝视角的系统洞察力。

---

## 0 摘要

Genesis 可观测性方案的核心在于**标准化**与**一体化**。它基于 CNCF 的 OpenTelemetry 标准，确保了数据的通用性和未来的扩展性。通过 `clog`、`metrics` 和 `trace` 三个基础组件的协同工作，实现了：

- **自动关联**：日志自动携带 TraceID，实现 Log 与 Trace 的无缝跳转。
- **统一接入**：只需简单的初始化配置，即可接入 OTLP 标准的后端（如 Tempo, Jaeger）。
- **全链路追踪**：覆盖 HTTP、gRPC、Database、Redis、MQ 等多种组件。
- **开箱即用**：推荐使用轻量级、低成本的 LGTM 栈，适合从单体到微服务的全阶段。

---

## 1 背景：打破数据孤岛

在传统监控体系中，日志、指标和追踪往往是分离的：

- **Logs**：存储在 ELK 或文件中，只能看到离散的报错信息。
- **Metrics**：存储在 Prometheus 中，只能看到聚合后的数值曲线。
- **Traces**：存储在 Jaeger/Zipkin 中，只能看到调用链的时间条。

当生产环境出现问题时，开发者需要在三个系统之间来回切换，试图通过时间戳人工关联数据，效率极低。Genesis 的目标是打破这些数据孤岛，通过 **TraceID** 这根红线，将 Logs、Metrics 和 Traces 串联起来，实现"发现告警 -> 查看链路 -> 定位日志"的顺滑排查流程。

---

## 2 核心设计：基于 OpenTelemetry 的 LGTM 栈

Genesis 强烈推荐使用 Grafana Labs 推出的 **LGTM** 技术栈作为可观测性后端，它与 Genesis 组件配合完美：

| 支柱 | 作用 | 工具栈 | Genesis 组件 |
| :--- | :--- | :--- | :--- |
| **Trace** (链路追踪) | 追踪请求全路径 | **Tempo** (对象存储，低成本) | `genesis/trace` |
| **Metrics** (指标监控) | 监控系统负载/QPS | **Prometheus** / **Mimir** | `genesis/metrics` |
| **Logging** (日志) | 记录事件和错误 | **Loki** (轻量级，无索引内容) | `genesis/clog` |
| **Visualization** | 统一展示界面 | **Grafana** | - |

### 2.1 组件协同

Genesis 的三个 L0 组件并非独立存在，而是深度耦合的：

1.  **Trace 组件**：负责初始化 OpenTelemetry SDK，配置采样率和 Exporter（通常是 OTLP gRPC）。
2.  **Clog 组件**：通过 `WithTraceContext` 选项，自动从 `context.Context` 中提取 `TraceID` 和 `SpanID`，注入到每条日志的字段中。
3.  **Metrics 组件**：提供标准的 Prometheus 指标暴露接口，并支持关联 Trace 上下文（Exemplar）。

---

## 3 实战落地：构建可观测的微服务

本节将基于 `examples/observability` 中的示例，展示如何在一个包含 HTTP 网关、gRPC 服务和 MQ 的微服务系统中落地可观测性。

### 3.1 统一初始化 (Bootstrap)

在 `main.go` 中，建议封装一个统一的初始化函数，一次性启动所有可观测性组件：

```go
func InitObservability(serviceName string) (func(context.Context) error, error) {
    // 1. 初始化 Trace (上报到 Tempo/Jaeger)
    shutdownTrace, err := trace.Init(&trace.Config{
        ServiceName: serviceName,
        Endpoint:    "localhost:4317", // OTLP gRPC
        Sampler:     1.0,              // 全量采集（生产环境建议降低）
        Insecure:    true,
    })
    if err != nil { return nil, err }

    // 2. 初始化 Metrics (暴露 /metrics 供 Prometheus 拉取)
    meter, err := metrics.New(metrics.NewDevDefaultConfig(serviceName))
    if err != nil { return nil, err }

    // 3. 初始化 Logger (关键：开启 Trace 关联)
    logger, _ := clog.New(
        &clog.Config{Level: "info", Format: "json"},
        clog.WithTraceContext(), // 自动注入 TraceID
    )
    
    // 返回统一的清理函数
    return func(ctx context.Context) error {
        _ = meter.Shutdown(ctx)
        return shutdownTrace(ctx)
    }, nil
}
```

### 3.2 链路追踪：HTTP 与 gRPC

Genesis 推荐使用 OpenTelemetry 官方中间件来实现自动的链路追踪。

**HTTP Gateway (Gin):**

```go
import "go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

r := gin.New()
// 使用 otelgin 中间件，自动提取 HTTP Header 中的 TraceParent
r.Use(otelgin.Middleware("gateway-service"))
```

**gRPC Server:**

```go
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

s := grpc.NewServer(
    // 使用 otelgrpc Handler，自动创建 Span 并传播 Context
    grpc.StatsHandler(otelgrpc.NewServerHandler()),
)
```

**gRPC Client:**

```go
conn, err := grpc.NewClient("target:9090",
    // 客户端也需要注入 Handler，将 Context 传给服务端
    grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
)
```

### 3.3 跨进程传播：MQ 消息队列

对于消息队列（如 NATS, Kafka），通常不支持自动透传 Header，需要手动进行 **Inject（注入）** 和 **Extract（提取）**。

**生产者 (Producer):**

```go
// 将当前 Context (含 TraceID) 注入到 Carrier (如 Header)
carrier := propagation.MapCarrier{}
otel.GetTextMapPropagator().Inject(ctx, carrier)

// 发送消息，将 carrier 作为元数据随消息发送
publish(topic, message, carrier)
```

**消费者 (Consumer):**

```go
// 从消息元数据中提取 Context
carrier := propagation.MapCarrier(msg.Header)
parentCtx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)

// 基于父 Context 开启新的 Span
tracer := otel.Tracer("consumer-service")
ctx, span := tracer.Start(parentCtx, "process_message")
defer span.End()

// 在新的 Context 下记录日志，会自动带上 TraceID
clog.InfoContext(ctx, "processing message", clog.String("msg_id", msg.ID))
```

---

## 4 可视化：Grafana 面板

一旦数据通过上述方式上报，Grafana 就能展现其强大的关联能力：

1.  **Trace View (Tempo)**: 查看请求的完整瀑布图。点击任意一个 Span，可以直接跳转到该时间段的 Logs。
2.  **Log View (Loki)**: 在查看日志时，每条日志旁边都有一个 "TraceID" 链接，点击即可跳转到对应的 Trace 视图。
3.  **Service Graph**: 基于 Trace 数据自动生成服务拓扑图，展示服务间的依赖关系、调用次数和延迟。

---

## 5 最佳实践

### 5.1 采样率控制

在生产环境中，全量采集 Trace 会带来巨大的存储成本和性能开销。建议：

- **开发/测试环境**：`Sampler: 1.0` (100%)，方便全量排查。
- **生产环境**：`Sampler: 0.01` (1%) 或更低，仅采集部分请求作为样本。
- **尾部采样 (Tail Sampling)**：这是更高级的策略（需在 OTel Collector 中配置），即"只保留出错或慢请求的 Trace"，丢弃正常的 Trace。

### 5.2 Context 传播是核心

Context 是 Go 语言并发编程的核心，也是可观测性的基石。**必须**确保 `context.Context` 在函数调用链中从头传到尾。一旦中断（例如使用了 `context.Background()` 替代了父 Context），Trace 链路就会断裂，日志也就失去了 TraceID 的关联。

### 5.3 结构化日志

坚持使用 `clog` 的结构化 API（如 `clog.String`, `clog.Int`），而不是拼接字符串。结构化日志让 Loki 能够高效地进行索引和查询，例如：`{service="order"} |= "error" | json | latency > 1s`。

---

## 6 总结

Genesis 的可观测性方案不是简单的工具堆砌，而是一套经过生产验证的方法论。通过标准化 OpenTelemetry 和 LGTM 栈，我们将原本复杂的监控体系简化为几个标准组件的组合。

对于开发者而言，只需要做两件事：**初始化组件** 和 **传递 Context**。剩下的——全链路追踪、指标聚合、日志关联——Genesis 都会自动帮你完成。
