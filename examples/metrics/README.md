# Metrics 示例 - Gin Web 服务

本示例演示如何在 Gin Web 框架中集成 Genesis Metrics 组件，实现 HTTP 请求的自动指标收集。

## 功能演示

这个示例包含：

1. **Metrics 初始化**
   - 创建 Metrics 配置
   - 初始化 Meter 实例
   - 创建自定义指标（Counter、Histogram、Gauge）

2. **HTTP 中间件埋点**
   - 自动记录所有请求的计数器（method、path、status）
   - 自动记录请求耗时分布（Histogram）
   - 实时跟踪活跃请求数（Gauge）

3. **业务路由**
   - GET `/` - 返回欢迎信息
   - POST `/orders` - 模拟创建订单
   - GET `/users/:id` - 获取用户信息
   - GET `/error` - 模拟错误响应

## 快速开始

### 前置条件

```bash
# 确保依赖已安装
go mod download
```

### 运行示例

```bash
cd examples/metrics
go run main.go
```

输出应该显示：

```
Starting Gin server on :8080
Starting client simulator...
Prometheus metrics available at http://localhost:9090/metrics
```

示例会自动启动：
1. **Gin HTTP 服务器** - 运行在 `:8080`
2. **客户端模拟器** - 自动每 3 秒发送一批测试请求
3. **Prometheus 指标导出** - 在 `:9090/metrics`

### 手动测试 API（可选）

如果需要手动测试，在另一个终端执行：

```bash
# GET 请求
curl http://localhost:8080/

# POST 请求
curl -X POST http://localhost:8080/orders \
  -H "Content-Type: application/json" \
  -d '{"name": "iPhone 15", "price": 999.99}'

# 带参数的 GET 请求
curl http://localhost:8080/users/123

# 错误响应
curl http://localhost:8080/error
```

### 查看指标

访问 Prometheus 指标端点：

```bash
curl http://localhost:9090/metrics
```

应该能看到类似的输出：

```text
# HELP http_requests_total Total HTTP requests
# TYPE http_requests_total counter
http_requests_total{method="GET",path="/",status="200"} 1
http_requests_total{method="POST",path="/orders",status="201"} 1
http_requests_total{method="GET",path="/users/123",status="200"} 1

# HELP http_request_duration_seconds HTTP request duration in seconds
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.005",method="GET",path="/"} 1
http_request_duration_seconds_bucket{le="0.01",method="GET",path="/"} 1
...

# HELP http_requests_active Number of active HTTP requests
# TYPE http_requests_active gauge
http_requests_active{method="GET"} 0
```

## 代码解析

### 1. 指标初始化

```go
cfg := &metrics.Config{
    Enabled:     true,
    ServiceName: "gin-demo",
    Version:     "v1.0.0",
    Port:        9090,              // Prometheus 端口
    Path:        "/metrics",        // Prometheus 路径
}

meter, err := metrics.New(cfg)
defer meter.Shutdown(ctx)
```

### 2. 创建自定义指标

```go
// Counter：计数器（只增不减）
requestCounter, _ := meter.Counter(
    "http_requests_total",
    "Total HTTP requests",
)

// Histogram：直方图（记录分布）
requestDuration, _ := meter.Histogram(
    "http_request_duration_seconds",
    "HTTP request duration in seconds",
)

// Gauge：仪表盘（可增可减）
activeRequests, _ := meter.Gauge(
    "http_requests_active",
    "Number of active HTTP requests",
)
```

### 3. 中间件埋点

```go
func metricsMiddleware(counter metrics.Counter, duration metrics.Histogram, active metrics.Gauge) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx := c.Request.Context()

        // 增加活跃请求
        active.Inc(ctx, metrics.L("method", c.Request.Method))

        // 记录耗时
        start := time.Now()
        defer func() {
            elapsed := time.Since(start).Seconds()

            // 记录计数器
            counter.Inc(ctx,
                metrics.L("method", c.Request.Method),
                metrics.L("path", c.Request.URL.Path),
                metrics.L("status", strconv.Itoa(c.Writer.Status())),
            )

            // 记录直方图
            duration.Record(ctx, elapsed,
                metrics.L("method", c.Request.Method),
                metrics.L("path", c.Request.URL.Path),
            )

            // 减少活跃请求
            active.Dec(ctx, metrics.L("method", c.Request.Method))
        }()

        c.Next()
    }
}
```

## 指标详解

### http_requests_total (Counter)

**类型**：Counter（只增）

**标签**：

- `method`：HTTP 方法 (GET, POST, etc)
- `path`：URL 路径
- `status`：HTTP 状态码

**示例**：

```
http_requests_total{method="POST",path="/orders",status="201"} 5
```

表示：有 5 个 POST /orders 请求返回 201 状态码

### http_request_duration_seconds (Histogram)

**类型**：Histogram（分布）

**标签**：

- `method`：HTTP 方法
- `path`：URL 路径

**输出格式**：

```
http_request_duration_seconds_bucket{le="0.005",method="GET",path="/"} 1
http_request_duration_seconds_bucket{le="0.01",method="GET",path="/"} 2
http_request_duration_seconds_sum{method="GET",path="/"} 0.012
http_request_duration_seconds_count{method="GET",path="/"} 2
```

- `_bucket{le="X"}`：耗时 ≤ X 秒的请求数
- `_sum`：所有请求耗时总和
- `_count`：所有请求总数

### http_requests_active (Gauge)

**类型**：Gauge（可增可减）

**标签**：

- `method`：HTTP 方法

**示例**：

```
http_requests_active{method="GET"} 2
```

表示：当前有 2 个 GET 请求在处理中

## Prometheus + Grafana 可视化

本示例提供了完整的 Docker Compose 配置，包含 Prometheus 和 Grafana，用于实时监控指标。

### 快速启动

#### 1. 启动示例应用

```bash
cd examples/metrics
go run main.go
```

应用会在以下端口运行：
- **Gin 服务** - http://localhost:8080
- **Prometheus 指标** - http://localhost:9090/metrics

#### 2. 启动 Prometheus 和 Grafana（Docker）

在另一个终端：

```bash
cd examples/metrics
docker-compose up
```

Docker 容器启动后：
- **Prometheus** - http://localhost:9091
- **Grafana** - http://localhost:3000

### Prometheus 查询

访问 http://localhost:9091，在查询框中输入以下 PromQL 表达式查看指标：

**请求总数**
```promql
rate(http_requests_total[1m])
```

**活跃请求数**
```promql
http_requests_active
```

**请求耗时（P95）**
```promql
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[1m]))
```

**按状态码分组的请求数**
```promql
sum(rate(http_requests_total[1m])) by (status)
```

**按路径分组的请求数**
```promql
sum(rate(http_requests_total[1m])) by (path)
```

### Grafana 可视化（推荐）

#### 🚀 快速方法（一键导入）

**第 1 步：登录 Grafana**
1. 访问 http://localhost:3000
2. 用户名: `admin` | 密码: `admin`

**第 2 步：导入预配置仪表板**
1. 左侧菜单 → **Dashboards** → 点击 **+ 导入**
2. 选择 **上传 JSON 文件**
3. 选择 `examples/metrics/grafana-dashboard.json`
4. 点击 **导入**

✅ 完成！已为您自动生成中文仪表板，包含 4 个面板：
- 📈 **请求速率** - 每秒请求数
- 🔄 **活跃请求数** - 当前处理的请求
- ⏱️ **请求延迟** - P95 和 P99 延迟
- 📊 **按状态码分布** - 请求状态统计

#### 手动配置方法（仅供参考）

如果需要自己配置，请按以下步骤：

**第 1 步：添加 Prometheus 数据源**
1. 左侧菜单 → **Connections** → **Data sources**
2. 点击 **Add data source**
3. 选择 **Prometheus**
4. URL: `http://prometheus:9090`
5. 点击 **Save & test**

**第 2 步：创建新仪表板**
1. 左侧菜单 → **Dashboards** → **Create** → **New dashboard**
2. 点击 **Add visualization**
3. 选择 **Prometheus** 数据源
4. 输入 PromQL 表达式

**常用 PromQL 查询**
| 名称 | 查询语句 | 说明 |
|------|--------|------|
| 请求速率 | `rate(http_requests_total[1m])` | 每秒请求数 |
| 活跃请求 | `http_requests_active` | 当前活跃请求数 |
| P95 延迟 | `histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[1m]))` | 95% 请求延迟 |
| P99 延迟 | `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[1m]))` | 99% 请求延迟 |
| 状态分布 | `sum(rate(http_requests_total[1m])) by (status)` | 按 HTTP 状态码分组 |
| 路径分布 | `sum(rate(http_requests_total[1m])) by (path)` | 按 URL 路径分组 |

### 配置文件说明

#### docker-compose.yml

本示例包含两个服务：

- **Prometheus** - 指标收集和存储
  - 端口：9091 (外部) → 9090 (内部)
  - 配置：./prometheus.yml
  - 数据持久化：prometheus_data 卷

- **Grafana** - 数据可视化
  - 端口：3000
  - 默认账户：admin/admin
  - 数据持久化：grafana_data 卷

#### prometheus.yml

```yaml
global:
  scrape_interval: 5s      # 每 5 秒收集一次指标
  evaluation_interval: 5s

scrape_configs:
  - job_name: 'genesis-metrics'
    static_configs:
      - targets: ['host.docker.internal:9090']  # 宿主机上的应用
```

### 停止容器

```bash
docker-compose down
```

移除数据卷：
```bash
docker-compose down -v
```

### 完整工作流程

```bash
# 终端 1：启动示例应用
cd examples/metrics
go run main.go

# 终端 2：启动 Docker 容器（等待应用启动后）
cd examples/metrics
docker-compose up

# 浏览器：
# 1. http://localhost:9090 - Prometheus 原生 UI
# 2. http://localhost:3000 - Grafana 仪表板（admin/admin）

# 观看指标更新（客户端模拟器每 3 秒发送一批请求）
```

### 故障排除

**Prometheus 无法连接到应用**

如果在 Prometheus 中看到 "DOWN" 状态，检查：
1. 应用是否正在运行（http://localhost:8080）
2. Prometheus 指标是否可访问（http://localhost:9090/metrics）
3. Docker 网络配置（使用 `host.docker.internal` 连接宿主机）

**Grafana 无法连接到 Prometheus**

1. 检查数据源配置中的 URL 是否为 `http://prometheus:9090`
2. 确保 Prometheus 容器在运行
3. 重启 Grafana 容器

**查看实时指标**

访问 http://localhost:9091 在 Graph 标签查看实时指标变化。

## 最佳实践

### ✅ 应该做

```go
// 1. 使用有意义的指标名
counter, _ := meter.Counter("orders_created_total", "Total orders created")

// 2. Label 使用固定值
counter.Inc(ctx,
    metrics.L("status", "success"),  // ✅ 固定值
    metrics.L("type", "standard"),   // ✅ 固定值
)

// 3. 使用合适的指标类型
// Counter：只增不减的计数
// Gauge：可增可减的仪表
// Histogram：分布式数据
```

### ❌ 不应该做

```go
// 1. 使用高基数 Label
counter.Inc(ctx,
    metrics.L("user_id", userID),  // ❌ 高基数！
    metrics.L("order_id", orderID), // ❌ 高基数！
)

// 2. 用 Counter 记录内存使用
memUsage, _ := meter.Counter("memory_usage", "Memory")  // ❌ 应该用 Gauge

// 3. 在 Histogram 中频繁创建新指标
for i := 0; i < 1000; i++ {
    h, _ := meter.Histogram("custom_"+i, "...")  // ❌ 会导致内存溢出
}
```

## 参考

- [Metrics 设计文档](../../docs/foundation/metrics-design.md)
- [OpenTelemetry](https://opentelemetry.io/)
- [Prometheus](https://prometheus.io/)
- [Gin Web Framework](https://gin-gonic.com/)
