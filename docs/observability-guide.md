# 实战：为分布式 IM 系统构建全栈可观测性 (Observability)

在开发分布式微服务（尤其是 IM 系统）时，随着组件（Redis, MySQL, Etcd）和服务节点（Gateway, Logic, Comet）的增加，排查问题变得极其困难。

"消息发了没收到？"
"是发消息慢，还是存离线消息慢？"
"某个节点是不是内存泄漏了？"

本文将基于 `Genesis` 组件库（`trace`, `clog`, `metrics`），指导你搭建一套符合云原生标准、适合个人独立开发者维护的全栈可观测性方案。

---

## 1. 架构概览

我们的目标是实现 **"三支柱" (Three Pillars)** 的统一：

| 支柱 | 作用 | 工具栈 | Genesis 组件 |
| :--- | :--- | :--- | :--- |
| **Trace** (链路追踪) | 追踪请求在微服务间流转的全路径 | **Tempo** (轻量级) / Jaeger | `genesis/trace` |
| **Metrics** (指标监控) | 监控系统负载、QPS、延迟分布 | **Prometheus** + **Grafana** | `genesis/metrics` |
| **Logging** (日志) | 记录离散的事件和错误详情 | **Loki** + **Promtail** | `genesis/clog` |

**为什么适合独立开发者？**
- **低资源消耗**：Tempo 和 Loki 比传统的 ELK 和 Jaeger 更加轻量，存储成本低。
- **一体化**：通过 TraceID 将日志、指标、链路串联起来，Grafana 一个界面看所有。

---

## 2. 基础设施搭建 (Docker Compose)

首先，我们需要启动监控后端。在你的项目根目录创建或更新 `docker-compose.yml`：

```yaml
version: "3.8"

services:
  # --- 1. 日志聚合 (Loki) ---
  loki:
    image: grafana/loki:latest
    ports: ["3100:3100"]
    command: -config.file=/etc/loki/local-config.yaml

  # --- 2. 链路追踪 (Tempo) ---
  tempo:
    image: grafana/tempo:latest
    ports:
      - "3200:3200"   # HTTP 查询
      - "4317:4317"   # OTLP gRPC (接收 Go 程序上报)
    command: [ "-config.file=/etc/tempo.yaml" ]
    volumes:
      - ./config/tempo.yaml:/etc/tempo.yaml

  # --- 3. 指标存储 (Prometheus) ---
  prometheus:
    image: prom/prometheus:latest
    ports: ["9090:9090"]
    command: --config.file=/etc/prometheus/prometheus.yml
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml

  # --- 4. 可视化 (Grafana) ---
  grafana:
    image: grafana/grafana:latest
    ports: ["3000:3000"]
    environment:
      - GF_AUTH_ANONYMOUS_ENABLED=true
    depends_on: [loki, tempo, prometheus]
```

*(注：需要配套的配置文件 `prometheus.yml`, `tempo.yaml` 等，参考 `examples/observability/config` 目录)*

---

## 3. Go 项目接入指南

### 3.1 初始化全家桶

在你的 `main.go` 中，一次性初始化所有组件：

```go
func main() {
    ctx := context.Background()

    // 1. 初始化 Trace (指向 Docker 中的 Tempo)
    shutdownTrace, err := trace.Init(&trace.Config{
        ServiceName: "im-logic-service",
        Endpoint:    "localhost:4317", // OTLP gRPC
        Sampler:     1.0,              // 全量采集(生产环境建议降低)
        Insecure:    true,
    })
    if err != nil { panic(err) }
    defer shutdownTrace(ctx)

    // 2. 初始化 Metrics (暴露 /metrics 供 Prometheus 拉取)
    metricsCfg := metrics.NewDevDefaultConfig("im-logic-service") // 包含 Go Runtime 指标
    meter, err := metrics.New(metricsCfg)
    if err != nil { panic(err) }
    defer meter.Shutdown(ctx)

    // 3. 初始化 Logger (关键：开启 Trace 关联)
    logger, _ := clog.New(
        &clog.Config{Level: "info"},
        clog.WithTraceContext(), // 自动在日志中注入 TraceID
    )
    
    // ... 启动业务代码
}
```

### 3.2 链路追踪：gRPC (自动)

Genesis 推荐使用 OpenTelemetry 官方中间件。

**Server 端:**
```go
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

s := grpc.NewServer(
    grpc.StatsHandler(otelgrpc.NewServerHandler()), // 自动创建 Span
)
```

**Client 端:**
```go
conn, err := grpc.NewClient("target:9090",
    grpc.WithStatsHandler(otelgrpc.NewClientHandler()), // 自动注入 Context
)
```

### 3.3 链路追踪：MQ 消息队列 (手动)

IM 系统中，消息投递通常经过 MQ (NATS/Kafka)。MQ 不像 HTTP/gRPC 会自动透传 Header，需要手动 "Inject" 和 "Extract"。

**生产者 (Producer):**
```go
// 1. 准备载体
headers := make(map[string]string)

// 2. 将当前 Context (含 TraceID) 注入到 headers
otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(headers))

// 3. 发送消息
kafkaClient.Send(topic, message, headers)
```

**消费者 (Consumer):**
```go
// 1. 从 headers 提取 Context
parentCtx := otel.GetTextMapPropagator().Extract(
    context.Background(),
    propagation.MapCarrier(msg.Headers),
)

// 2. 开启新的 Span (链接到父 Span)
tracer := otel.Tracer("im-consumer")
ctx, span := tracer.Start(parentCtx, "process_offline_msg")
defer span.End()

// 3. 业务逻辑 (此时 Log 会自动带上 TraceID)
logger.InfoContext(ctx, "processing message", clog.String("msg_id", msg.ID))
```

### 3.4 关键业务指标

除了 CPU/内存等系统指标，你应该关注业务指标：

```go
// 定义指标
var (
    msgLatency = metrics.MustHistogram("im_msg_latency", "消息投递耗时")
    onlineUsers = metrics.MustGauge("im_online_users", "当前在线人数")
)

// 记录数据
func HandleMessage(ctx context.Context) {
    start := time.Now()
    // ... 业务逻辑 ...
    
    // 记录耗时，带上标签
    msgLatency.Record(ctx, time.Since(start).Seconds(), 
        metrics.L("type", "private"), // 私聊
        metrics.L("status", "success"),
    )
}
```

---

## 4. 生产环境部署建议

1.  **日志收集**：
    *   在 Docker 环境下，不要将日志写文件。
    *   使用 `clog` 输出到 `stdout` (JSON格式)。
    *   部署 **Promtail** DaemonSet 或 Sidecar，采集容器 stdout 发送到 Loki。

2.  **采样率控制**：
    *   IM 系统消息量大，`Sampler: 1.0` (100%采样) 会导致存储爆炸。
    *   生产环境建议设为 `0.01` (1%) 或更低，或者使用 "尾部采样" (Tail Sampling) 策略。

3.  **TraceID 串联**：
    *   确保前端/客户端发起请求时生成 TraceID，并通过 Header 传给 Gateway。
    *   这样你可以追踪 "点击发送 -> 网关 -> 逻辑层 -> MQ -> 存储 -> 推送" 的完整全链路。

## 5. 总结

通过这一套方案，你将获得：

1.  **全局视野**：Grafana 面板一目了然看到系统 QPS 和 错误率。
2.  **快速定位**：发现错误 -> 复制 TraceID -> 在 Tempo 搜到全链路 -> 在 Loki 看到该链路所有日志。
3.  **无感接入**：基于 Genesis 的封装，大部分工作只需几行初始化代码。

这对于独立开发者来说，是性价比最高的 "企业级" 可观测性实践。
