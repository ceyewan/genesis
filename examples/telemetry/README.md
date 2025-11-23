# Genesis Telemetry 示例

这个示例展示了 Genesis 遥测系统的完整功能，包括指标收集、链路追踪、自动拦截器和多种导出器配置。

## 🎯 示例概述

本示例包含三个渐进式的演示：

1. **基础遥测配置** - 展示如何初始化和配置遥测系统
2. **高级遥测配置** - 演示自定义指标创建和追踪 span 的使用
3. **完整服务示例** - 综合展示 gRPC + HTTP + 指标 + 追踪的完整微服务场景

## 🚀 快速开始

### 方法 1：一键启动（推荐）

使用提供的启动脚本快速搭建完整的监控环境：

```bash
cd examples/telemetry
./start.sh
```

这个脚本会自动：

- ✅ 检查端口占用
- ✅ 构建应用镜像
- ✅ 启动所有服务（应用、Prometheus、Grafana、Jaeger）
- ✅ 验证服务状态
- ✅ 提供访问信息和示例命令

### 方法 2：Docker Compose 启动

```bash
cd examples/telemetry
docker-compose up -d
```

### 方法 3：手动运行

```bash
cd examples/telemetry
go run main.go
```

### 2. 访问服务

- **HTTP API**: <http://localhost:8080>
- **Prometheus 指标**: <http://localhost:9093/metrics>
- **健康检查**: <http://localhost:8080/api/v1/health>
- **指标信息**: <http://localhost:8080/api/v1/metrics/info>

### 3. 服务访问

一键启动后，你可以访问以下服务：

**核心服务：**

- 📊 **Prometheus**: <http://localhost:9090>
- 📈 **Grafana**: <http://localhost:3000> (admin/admin)
- 🔍 **Jaeger**: <http://localhost:16686>
- 🚀 **示例应用**: <http://localhost:8080>

**应用端点：**

- 📋 **应用指标**: <http://localhost:9093/metrics>
- 🏥 **健康检查**: <http://localhost:8080/api/v1/health>
- 📊 **指标信息**: <http://localhost:8080/api/v1/metrics/info>

### 4. 测试 API

创建订单：

```bash
curl -X POST http://localhost:8080/api/v1/orders/create \
  -H "Content-Type: application/json" \
  -d '{"user_id": 12345, "product": "iPhone", "amount": 999.99}'
```

查询订单状态：

```bash
curl http://localhost:8080/api/v1/orders/ORDER-12345-1234567890/status
```

取消订单：

```bash
curl -X PUT http://localhost:8080/api/v1/orders/ORDER-12345-1234567890/cancel
```

## 🐳 Docker 环境

### 快速启动

```bash
# 启动所有服务
./start.sh

# 查看服务状态
./start.sh status

# 查看日志
./start.sh logs

# 停止所有服务
./start.sh stop
```

### 手动 Docker 操作

```bash
# 构建镜像
docker-compose build

# 启动服务
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down
```

## 监控面板配置

### Prometheus 配置

### Grafana 仪表板导入

我们已经提供了完整的 Grafana 仪表板配置：

1. **自动导入**（使用 Docker Compose）：
   - 仪表板会自动导入到 Grafana
   - 访问 <http://localhost:3000> 查看

2. **手动导入**：
   - 下载 [`grafana-dashboard.json`](grafana-dashboard.json)
   - 在 Grafana 中导入该 JSON 文件
   - 选择 Prometheus 数据源

### Prometheus 配置

在你的 `prometheus.yml` 中添加以下配置：

```yaml
scrape_configs:
  - job_name: 'genesis-telemetry-example'
    static_configs:
      - targets: ['localhost:9093']
        labels:
          service: 'order-service'
          environment: 'demo'
```

### 预置监控面板

仪表板包含以下面板：

1. **请求速率** - 按操作和状态分类的请求速率
2. **错误率** - 实时错误率仪表盘
3. **响应时间分布** - P50/P95 响应时间
4. **活跃用户数** - 当前活跃用户数趋势
5. **消息大小分布** - 消息大小统计
6. **错误分类统计** - 按类型分类的错误统计

#### 1. 导入仪表板

导入 ID: `1860` (Node Exporter Full) 作为基础，然后添加自定义查询。

#### 2. 关键指标查询

**请求速率**

```promql
rate(order_requests_total[5m])
```

**错误率**

```promql
rate(order_errors_total[5m]) / rate(order_requests_total[5m]) * 100
```

**响应时间 P95**

```promql
histogram_quantile(0.95, rate(order_response_duration_seconds_bucket[5m]))
```

**活跃用户数**

```promql
active_users_total
```

#### 3. 链路追踪

配置 Jaeger 或 Zipkin 来收集追踪数据：

```bash
# 启动 Jaeger
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 14268:14268 \
  jaegertracing/all-in-one:latest
```

修改示例配置使用 Jaeger 导出器：

```go
cfg := &telemetry.Config{
    ServiceName:      "order-service",
    ExporterType:     "jaeger",
    ExporterEndpoint: "http://localhost:14268/api/traces",
    // ... 其他配置
}
```

## 🔧 配置选项

### 基础配置

```go
cfg := &telemetry.Config{
    ServiceName:          "your-service-name",     // 服务名称
    ExporterType:         "stdout",                // 导出器类型: stdout, jaeger, zipkin
    ExporterEndpoint:     "http://localhost:14268/api/traces", // 导出器端点
    PrometheusListenAddr: ":9090",                 // Prometheus 监听地址
    SamplerType:          "always_on",             // 采样策略: always_on, always_off, trace_id_ratio
    SamplerRatio:         0.1,                     // 采样比例 (0.0-1.0)
}
```

### 指标类型

**计数器 (Counter)**

```go
counter, _ := tel.Meter().Counter("requests_total", "Total requests")
counter.Inc(ctx, types.Label{Key: "status", Value: "success"})
```

**仪表盘 (Gauge)**

```go
gauge, _ := tel.Meter().Gauge("active_connections", "Active connections")
gauge.Set(ctx, 42, types.Label{Key: "type", Value: "websocket"})
```

**直方图 (Histogram)**

```go
hist, _ := tel.Meter().Histogram("response_duration_seconds", "Response duration", types.WithUnit("s"))
hist.Record(ctx, 0.125, types.Label{Key: "endpoint", Value: "/api/users"})
```

### 链路追踪

**创建 Span**

```go
ctx, span := tracer.Start(ctx, "operation-name", types.WithSpanKind(types.SpanKindServer))
defer span.End()
```

**设置属性**

```go
span.SetAttributes(
    types.Attribute{Key: "user.id", Value: "12345"},
    types.Attribute{Key: "request.size", Value: 1024},
)
```

**记录错误**

```go
span.RecordError(err)
span.SetStatus(types.StatusCodeError, "Operation failed")
```

## 🛠 集成指南

### 在现有服务中集成

1. **初始化遥测**

```go
import "github.com/ceyewan/genesis/pkg/telemetry"

func initTelemetry() (telemetry.Telemetry, error) {
    cfg := &telemetry.Config{
        ServiceName:          "my-service",
        ExporterType:         "jaeger",
        ExporterEndpoint:     "http://jaeger:14268/api/traces",
        PrometheusListenAddr: ":9090",
        SamplerType:          "trace_id_ratio",
        SamplerRatio:         0.1,
    }
    
    return telemetry.New(cfg)
}
```

2. **添加 HTTP 中间件**

```go
engine := gin.New()
engine.Use(tel.HTTPMiddleware())
```

3. **添加 gRPC 拦截器**

```go
server := grpc.NewServer(
    grpc.ChainUnaryInterceptor(tel.GRPCServerInterceptor()),
)
```

4. **创建自定义指标**

```go
meter := tel.Meter()
counter, _ := meter.Counter("business_events_total", "Business events")
```

5. **创建追踪 span**

```go
tracer := tel.Tracer()
ctx, span := tracer.Start(ctx, "business-operation")
defer span.End()
```

## 📈 性能考虑

### 采样策略

- **开发环境**: 使用 `always_on` 全采样
- **测试环境**: 使用 `trace_id_ratio` 10% 采样
- **生产环境**: 使用 `trace_id_ratio` 1% 或更低采样

### 指标收集

- 合理设置指标标签，避免标签值过多
- 使用直方图时注意桶的配置
- 定期清理不需要的指标

### 资源使用

- Prometheus 内存使用与指标数量成正比
- 追踪数据存储需要考虑采样率
- 建议设置合理的指标和追踪数据保留期

## 🔍 故障排查

### 常见问题

1. **指标不显示**
   - 检查 Prometheus 是否正确抓取
   - 确认指标名称和标签
   - 查看应用日志是否有错误

2. **追踪数据缺失**
   - 检查采样配置
   - 确认导出器端点可达
   - 查看导出器日志

3. **性能问题**
   - 检查采样率设置
   - 监控指标数量增长
   - 优化追踪 span 创建

### 调试技巧

启用详细日志：

```go
logger, _ := clog.New(&clogtypes.Config{
    Level:  "debug",
    Format: "console",
    Output: "stdout",
}, nil)
```

检查指标端点：

```bash
curl http://localhost:9090/metrics | grep your_metric_name
```

验证追踪导出：

```bash
# 使用 stdout 导出器查看追踪数据
curl -X POST http://localhost:8080/api/endpoint -H "traceparent: 00-1234567890abcdef1234567890abcdef-1234567890abcdef-01"
```

## 📚 相关资源

### 官方文档

- [OpenTelemetry 官方文档](https://opentelemetry.io/docs/)
- [Prometheus 查询语言](https://prometheus.io/docs/prometheus/latest/querying/basics/)
- [Jaeger 追踪系统](https://www.jaegertracing.io/docs/)
- [Grafana 仪表板](https://grafana.com/docs/)

### Genesis 文档

- [Genesis 遥测设计文档](../../docs/telemetry-design.md)
- [Genesis 项目主页](https://github.com/ceyewan/genesis)

### 最佳实践文章

- [分布式追踪最佳实践](https://opentelemetry.io/docs/concepts/distributed-tracing/)
- [Prometheus 监控最佳实践](https://prometheus.io/docs/practices/)
- [Grafana 仪表板设计](https://grafana.com/docs/grafana/latest/best-practices/best-practices-for-creating-dashboards/)

- [OpenTelemetry 官方文档](https://opentelemetry.io/docs/)
- [Prometheus 查询语言](https://prometheus.io/docs/prometheus/latest/querying/basics/)
- [Jaeger 追踪系统](https://www.jaegertracing.io/docs/)
- [Grafana 仪表板](https://grafana.com/docs/)

## 💡 最佳实践

1. **统一命名规范**: 使用一致的服务名称和指标命名
2. **合理采样**: 根据环境调整采样率
3. **标签设计**: 精心设计的标签便于后续分析
4. **监控告警**: 基于关键指标设置告警规则
5. **定期审查**: 定期审查和优化遥测配置

这个示例为你提供了完整的遥测系统使用指南，帮助你构建可观测的微服务架构。
