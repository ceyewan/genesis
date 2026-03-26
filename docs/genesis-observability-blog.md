# Genesis observability：Metrics、Trace 与 Logging 的综合实践

Genesis 的可观测性体系不是一个单独组件，而是 `clog`、`metrics` 和 `trace` 三个 L0 组件协同工作的结果。它面向微服务和组件库场景，重点解决三类问题：如何在应用启动时稳定初始化可观测性、如何在 HTTP / gRPC / MQ / DB 场景中保持上下文一致，以及如何把日志、指标和链路串成一条可执行的排障路径。这篇文章不是组件源码讲解，而是一篇面向落地的综合实践文。

---

## 0 摘要

- `trace` 负责安装全局 tracing 状态，并为 HTTP、gRPC、MQ 提供统一传播路径
- `clog` 负责把 `trace_id`、`span_id` 等上下文字段稳定注入结构化日志
- `metrics` 负责统一服务端指标接口、Prometheus 暴露与 RED 埋点约定
- 三者通过 `context.Context` 协同，而不是靠手工拼时间戳或人工比对日志
- 推荐后端是 LGTM 栈，但 Genesis 的重点不是绑定某套后端，而是先把接入方式标准化

---

## 1 背景与目标

可观测性最容易陷入的误区，是把日志、指标和 trace 当作三套互不相干的工具。结果通常是：日志里有报错但缺少上下文，指标能看到异常但不知道是哪类请求造成的，trace 能看到链路但跳不到对应日志。真正的生产排障并不是三套系统分别“可用”，而是三者能否围绕同一条请求上下文协同。

Genesis 的目标不是再发明一套可观测性平台，而是给出一套统一接入和统一实践：应用启动时明确初始化 tracing 和 metrics，业务日志通过 `context.Context` 自动关联 trace 字段，HTTP / gRPC / MQ / DB 等路径都尽量复用同一套上下文传播方式。最终让“发现异常 -> 找到具体链路 -> 查看相关日志 -> 回看指标波动”这条路径尽量顺滑。

---

## 2 Genesis 可观测性的整体分工

Genesis 推荐把可观测性拆成三个明确角色，而不是堆成一个大模块：

| 支柱                   | 作用             | 工具栈                        | Genesis 组件      |
| :--------------------- | :--------------- | :---------------------------- | :---------------- |
| **Trace** (链路追踪)   | 追踪请求全路径   | **Tempo** (对象存储，低成本)  | `genesis/trace`   |
| **Metrics** (指标监控) | 监控系统负载/QPS | **Prometheus** / **Mimir**    | `genesis/metrics` |
| **Logging** (日志)     | 记录事件和错误   | **Loki** (轻量级，无索引内容) | `genesis/clog`    |
| **Visualization**      | 统一展示界面     | **Grafana**                   | -                 |

这里最关键的不是工具名字，而是分工边界：

- `trace` 负责安装全局 `TracerProvider` 和传播器，并把 HTTP、gRPC、MQ 的传播路径接起来
- `clog` 负责把上下文里的 trace 字段稳定打进日志，让日志和链路之间能互相跳转
- `metrics` 负责暴露服务端指标和统一指标标签，使聚合查询和告警规则更稳定

这三者之间真正的连接点是 `context.Context`。只要上下文能从入口一路传到下游，trace 可以延续，日志可以带上 `trace_id`，业务埋点和服务端指标也可以在同一个请求范围内组织起来。

---

## 3 启动顺序与初始化方式

可观测性最容易出问题的地方不是“某个 API 不会用”，而是初始化顺序不一致。Genesis 推荐把它收敛成固定的 bootstrap 流程：先初始化 tracing，再初始化 metrics，再初始化 logger，最后把这三者注入到业务组件。

原因很直接：

- `trace` 当前采用全局模式，应该尽早安装 provider 和 propagator
- `metrics` 当前也采用全局模式，适合在应用 bootstrap 阶段只初始化一次
- `clog` 只有在 tracing 状态已经就位后，`WithTraceContext` 才能稳定提取 trace 字段

推荐初始化模式如下：

```go
func InitObservability(serviceName string) (func(context.Context) error, error) {
    shutdownTrace, err := trace.Init(&trace.Config{
        ServiceName: serviceName,
        Endpoint:    "localhost:4317",
        Sampler:     1.0,
        Insecure:    true,
    })
    if err != nil { return nil, err }

    meter, err := metrics.New(metrics.NewDevDefaultConfig(serviceName))
    if err != nil { return nil, err }

    logger, _ := clog.New(
        &clog.Config{Level: "info", Format: "json"},
        clog.WithTraceContext(),
    )

    return func(ctx context.Context) error {
        _ = meter.Shutdown(ctx)
        return shutdownTrace(ctx)
    }, nil
}
```

这里的重点不是把三段代码写在一起，而是把它们纳入统一生命周期。应用退出时，`trace` 和 `metrics` 的 shutdown 都应该被显式调用；logger 则按各自组件约定释放资源。

---

## 4 HTTP 场景实践

HTTP 通常是请求进入系统的第一站，因此它既是 tracing 的入口，也是日志和服务端指标最自然的汇合点。

推荐做法是：

- 用 `trace.GinMiddleware()` 建立请求级 span
- 用 `metrics.GinHTTPMiddleware()` 记录服务端 RED 指标
- 用 `clog.WithTraceContext()` 让日志自动带 `trace_id`
- 在业务处理函数里坚持把 `ctx` 往下传，而不是重新起 `context.Background()`

示例：

```go
r := gin.New()
r.Use(trace.GinMiddleware("gateway-service"))
r.Use(metrics.GinHTTPMiddleware(httpMetrics))
```

这样做的结果是：

- trace 里能看到入口 span
- 指标里能看到按路由聚合的请求量和耗时
- 日志里能直接带出对应的 `trace_id`

如果这三者缺一个，排障链路都会断一截。

---

## 5 gRPC 场景实践

gRPC 和 HTTP 的思路一样，关键仍然是统一传播和统一埋点，而不是每个 client / server 自己决定要不要带 tracing。

服务端：

```go
s := grpc.NewServer(
    grpc.StatsHandler(trace.GRPCServerStatsHandler()),
    grpc.UnaryInterceptor(grpcMetrics.UnaryServerInterceptor()),
)
```

客户端：

```go
conn, err := grpc.NewClient(
    "target:9090",
    grpc.WithStatsHandler(trace.GRPCClientStatsHandler()),
)
```

这里最容易忽略的是：**gRPC client 也要装 tracing handler**。很多团队只在 server 侧装，结果链路在服务边界处断掉，或者只能看到 server span，看不到上游 client span。

---

## 6 MQ 场景实践

异步消息是可观测性最容易断链的地方，因为它不像 HTTP 和 gRPC 那样天然有标准传播路径。Genesis 在这里给出的实践不是“自动魔法”，而是显式 helper：生产端注入，消费端提取，再显式选择关系建模。

生产端：

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
```

---

消费端：

```go
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

默认推荐用 `link`，因为它更符合真实异步系统。只有在确实想把整条路径串成一条线性 trace 用于演示或特定排障时，才考虑 `child_of`。

---

## 7 日志、指标与链路如何联动排障

真正的生产排障通常不是“先看哪个系统”，而是从异常入口开始不断缩小范围。一个更实用的顺序通常是：

1. 先从指标发现异常，例如接口耗时升高、错误率上升、某个 gRPC 方法超时增多
2. 再进入对应 trace，看问题集中在哪个 span、哪个下游依赖、哪个异步处理环节
3. 最后通过 `trace_id` 去日志里拿到更细的业务上下文和错误字段

这条链路能否成立，依赖三个前提：

- tracing 已经在入口和跨进程边界上接起来
- 日志已经稳定带上 `trace_id`
- 指标标签足够统一，能把异常聚合到正确的服务、方法和路由维度

Genesis 的作用不是替你做诊断，而是把这三条路径的接入方式标准化，让你在出问题时不必再先解决“数据为什么对不上”。

---

## 8 推荐部署方式与运行建议

Genesis 推荐使用 LGTM 栈承载这三类数据：

- Loki 存日志
- Prometheus 或 Mimir 存指标
- Tempo 存 trace
- Grafana 统一看板和跳转

但这并不意味着 Genesis 绑定某一套后端。真正重要的是：无论后端换不换，前端接入仍然基于 OpenTelemetry 和稳定的日志字段契约。后端可以替换，接入方式不应每个服务都重写。

运行上有三条建议：

- 开发环境可以提高 trace 采样率，方便完整排查
- 生产环境应把 trace 采样率控制在可接受范围内，不要默认全量
- 把 `/metrics`、tracing 初始化失败和 logger 资源关闭都纳入统一 bootstrap / shutdown 流程

---

## 9 常见误区

- 只初始化 tracing，不把 `trace_id` 打进日志
- 只装服务端中间件，不装客户端传播
- 在 MQ 场景里默认把所有消费都建模成 `child_of`
- 把原始 URL path 或用户 ID 直接打成指标标签
- 在业务中途随意重建全局 `metrics` 或 `trace` 状态

---

## 10 总结

Genesis 的可观测性实践不是把日志、指标和 trace 三套工具简单堆在一起，而是通过统一初始化、统一上下文传播和统一字段约定，把它们组织成一套可以真正支持排障的工程体系。

如果要用一句话总结这套实践的核心，那就是：**先让三类数据围绕同一条请求上下文对齐，再谈平台、看板和高级分析能力。**
