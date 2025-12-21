# Breaker 组件

Breaker 是 Genesis 微服务框架的熔断器组件，专注于 gRPC 客户端的故障隔离与自动恢复。基于 [gobreaker](https://github.com/sony/gobreaker) 实现，提供轻量级、高性能的熔断保护能力。

## 特性

- ✅ **故障隔离** - 当下游服务频繁失败时，自动熔断请求，避免级联故障
- ✅ **自动恢复** - 通过半开状态定期探测，在下游服务恢复后自动闭合熔断器
- ✅ **轻量化** - 单机模式，基于内存统计，无外部依赖，启动快、性能高
- ✅ **服务级粒度** - 按目标服务名熔断，不同服务独立管理，互不影响
- ✅ **灵活降级** - 支持直接快速失败和自定义降级逻辑两种策略
- ✅ **无侵入集成** - 提供 gRPC Interceptor，业务代码无需修改即可接入熔断保护
- ✅ **指标埋点** - 完整的可观测性支持（OpenTelemetry）
- ✅ **错误处理** - 统一的错误定义与处理

## 目录结构（完全扁平化设计）

```
breaker/
├── breaker.go          # 核心接口、配置与工厂函数
├── implementation.go   # 熔断器实现
├── interceptor.go      # gRPC 拦截器
├── options.go          # 初始化选项函数
├── errors.go           # 错误定义（使用 xerrors）
├── metrics.go          # 指标常量定义
└── README.md           # 本文件
```

**设计原则**：完全扁平化设计，所有公开 API 和实现都在根目录，无 `types/` 子包

## 快速开始

### 基本使用

```go
import (
    "context"
    "time"
    "github.com/ceyewan/genesis/breaker"
    "github.com/ceyewan/genesis/clog"
    "google.golang.org/grpc"
)

// 创建 Logger
logger, _ := clog.New(&clog.Config{Level: "info"})

// 创建熔断器
brk, _ := breaker.New(&breaker.Config{
    MaxRequests:     5,              // 半开状态允许 5 个探测请求
    Interval:        60 * time.Second, // 60 秒统计周期
    Timeout:         30 * time.Second, // 熔断 30 秒后进入半开状态
    FailureRatio:    0.6,             // 失败率 60% 触发熔断
    MinimumRequests: 10,              // 至少 10 个请求才触发熔断
}, breaker.WithLogger(logger))

// 使用 gRPC Interceptor
conn, _ := grpc.Dial(
    "localhost:9001",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
    grpc.WithStreamInterceptor(brk.StreamClientInterceptor()),
)
defer conn.Close()

// 正常使用 gRPC 客户端，熔断器会自动保护
client := pb.NewYourServiceClient(conn)
resp, err := client.YourMethod(context.Background(), &pb.Request{})
```

### 自定义降级逻辑

```go
// 创建带降级逻辑的熔断器
brk, _ := breaker.New(&breaker.Config{
    MaxRequests:     5,
    Timeout:         30 * time.Second,
    FailureRatio:    0.6,
    MinimumRequests: 10,
},
    breaker.WithLogger(logger),
    breaker.WithFallback(func(ctx context.Context, serviceName string, err error) error {
        // 返回缓存数据或默认值
        logger.Warn("circuit breaker open, using fallback",
            clog.String("service", serviceName),
            clog.Error(err))
        // 返回 nil 表示降级成功
        return nil
    }),
)
```

## 核心接口

### Breaker 接口

```go
type Breaker interface {
    // Execute 执行受熔断保护的函数
    Execute(ctx context.Context, serviceName string, fn func() (interface{}, error)) (interface{}, error)

    // UnaryClientInterceptor 返回 gRPC 一元调用客户端拦截器
    UnaryClientInterceptor() grpc.UnaryClientInterceptor

    // StreamClientInterceptor 返回 gRPC 流式调用客户端拦截器
    StreamClientInterceptor() grpc.StreamClientInterceptor

    // State 获取指定服务的熔断器状态
    State(serviceName string) (State, error)
}
```

### State 状态

```go
const (
    StateClosed   State = iota // 闭合状态（正常）
    StateHalfOpen              // 半开状态（探测恢复）
    StateOpen                  // 打开状态（熔断中）
)
```

## 配置结构

### Config

```go
type Config struct {
    // MaxRequests 半开状态下允许通过的最大请求数（默认：1）
    MaxRequests uint32

    // Interval 闭合状态下的统计周期（默认：0，不清空统计）
    Interval time.Duration

    // Timeout 打开状态持续时间（默认：60s）
    Timeout time.Duration

    // FailureRatio 失败率阈值（默认：0.6，即 60%）
    FailureRatio float64

    // MinimumRequests 触发熔断的最小请求数（默认：10）
    MinimumRequests uint32
}
```

## 应用场景

### 1. gRPC 客户端保护

```go
// 为 gRPC 客户端添加熔断保护
conn, _ := grpc.Dial(
    "user-service:9001",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
)

client := pb.NewUserServiceClient(conn)
// 自动受熔断保护
resp, err := client.GetUser(ctx, &pb.GetUserRequest{ID: "123"})
```

### 2. 直接使用 Execute 方法

```go
// 保护任意函数调用
result, err := brk.Execute(ctx, "payment-service", func() (interface{}, error) {
    return paymentClient.ProcessPayment(ctx, req)
})
```

### 3. 查询熔断器状态

```go
state, err := brk.State("user-service")
if err != nil {
    logger.Error("failed to get breaker state", clog.Error(err))
}

switch state {
case breaker.StateClosed:
    logger.Info("circuit breaker is closed (normal)")
case breaker.StateHalfOpen:
    logger.Info("circuit breaker is half-open (probing)")
case breaker.StateOpen:
    logger.Warn("circuit breaker is open (circuit broken)")
}
```

## 工作原理

### 熔断器状态机

```
┌─────────┐
│ Closed  │ ◄──────────────────┐
│ (正常)  │                    │
└────┬────┘                    │
     │                         │
     │ 失败率 >= 阈值           │ 成功请求 >= MaxRequests
     │                         │
     ▼                         │
┌─────────┐                ┌──┴──────┐
│  Open   │───────────────►│HalfOpen │
│ (熔断)  │   Timeout后     │ (探测)  │
└─────────┘                └─────────┘
```

### 状态说明

1. **Closed（闭合状态）**
   - 正常状态，所有请求正常通过
   - 统计请求成功/失败次数
   - 当失败率超过阈值时，转换到 Open 状态

2. **Open（打开状态）**
   - 熔断状态，所有请求被快速拒绝
   - 不会真正调用下游服务
   - 可以执行降级逻辑
   - Timeout 时间后，转换到 HalfOpen 状态

3. **HalfOpen（半开状态）**
   - 探测状态，允许少量请求通过
   - 如果请求成功，转换到 Closed 状态
   - 如果请求失败，转换回 Open 状态

## 配置说明

### 参数详解

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `MaxRequests` | uint32 | 1 | 半开状态下允许通过的最大请求数 |
| `Interval` | time.Duration | 0 | 闭合状态下的统计周期（0 表示不清空） |
| `Timeout` | time.Duration | 60s | 打开状态持续时间 |
| `FailureRatio` | float64 | 0.6 | 失败率阈值（0.0-1.0） |
| `MinimumRequests` | uint32 | 10 | 触发熔断的最小请求数 |

### 配置建议

**高可用场景**（宽松熔断）：
```go
&breaker.Config{
    MaxRequests:     10,
    Timeout:         30 * time.Second,
    FailureRatio:    0.7,  // 70% 失败率才熔断
    MinimumRequests: 20,   // 至少 20 个请求
}
```

**快速失败场景**（严格熔断）：
```go
&breaker.Config{
    MaxRequests:     3,
    Timeout:         10 * time.Second,
    FailureRatio:    0.5,  // 50% 失败率就熔断
    MinimumRequests: 5,    // 只需 5 个请求
}
```

## 指标说明

### 可用指标

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `breaker_requests_total` | Counter | service, method, result | 请求总数 |
| `breaker_success_total` | Counter | service | 成功请求数 |
| `breaker_failures_total` | Counter | service | 失败请求数 |
| `breaker_rejects_total` | Counter | service | 被熔断拒绝的请求数 |
| `breaker_state_changes_total` | Counter | service, from_state, to_state | 状态变更次数 |
| `breaker_request_duration_seconds` | Histogram | service | 请求耗时 |

### Prometheus 查询示例

```promql
# 查看各服务的熔断拒绝率
rate(breaker_rejects_total[1m]) / rate(breaker_requests_total[1m])

# 查看状态变更频率
rate(breaker_state_changes_total{to_state="open"}[5m])

# 查看请求成功率
rate(breaker_success_total[1m]) / rate(breaker_requests_total[1m])
```

## 最佳实践

### 1. 合理设置阈值

```go
// ❌ 错误：阈值过低，容易误触发
&breaker.Config{
    FailureRatio:    0.1,  // 10% 失败率就熔断，太敏感
    MinimumRequests: 2,    // 只需 2 个请求，样本太少
}

// ✅ 正确：合理的阈值
&breaker.Config{
    FailureRatio:    0.5,  // 50% 失败率才熔断
    MinimumRequests: 10,   // 至少 10 个请求，样本充足
}
```

### 2. 使用降级逻辑

```go
// ✅ 推荐：提供降级逻辑，提升用户体验
brk, _ := breaker.New(cfg,
    breaker.WithFallback(func(ctx context.Context, serviceName string, err error) error {
        // 返回缓存数据
        cachedData := cache.Get(ctx, "user:123")
        if cachedData != nil {
            return nil
        }
        // 返回默认值
        return nil
    }),
)
```

### 3. 监控和告警

```yaml
# Prometheus 告警规则示例
groups:
  - name: breaker
    rules:
      - alert: CircuitBreakerOpen
        expr: breaker_state_changes_total{to_state="open"} > 0
        for: 1m
        annotations:
          summary: "熔断器打开: {{ $labels.service }}"

      - alert: HighRejectRate
        expr: rate(breaker_rejects_total[5m]) / rate(breaker_requests_total[5m]) > 0.5
        for: 2m
        annotations:
          summary: "熔断拒绝率过高: {{ $labels.service }}"
```

### 4. 日志记录

```go
// 注入 Logger 以便追踪熔断器行为
brk, _ := breaker.New(cfg, breaker.WithLogger(logger))

// 日志会自动记录：
// - 熔断器创建
// - 状态变更（closed -> open -> half_open -> closed）
// - 熔断拒绝
```

## 错误处理

### 错误类型

```go
// ErrConfigNil - 配置为空
if err == breaker.ErrConfigNil {
    // 处理配置错误
}

// ErrServiceNameEmpty - 服务名为空
if err == breaker.ErrServiceNameEmpty {
    // 处理服务名错误
}

// ErrOpenState - 熔断器打开
if errors.Is(err, breaker.ErrOpenState) {
    // 执行降级逻辑
}
```

## 运行示例

```bash
# 运行示例
go run examples/breaker/main.go

# 查看 Prometheus 指标
curl http://localhost:9090/metrics | grep breaker
```

## 与其他组件集成

### 与 Registry 集成

```go
// 使用服务发现 + 熔断器
conn, _ := registry.GetConnection(ctx, "user-service")
conn = grpc.Dial(
    conn.Target(),
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
)
```

### 与 RateLimit 集成

```go
// 限流 + 熔断双重保护
conn, _ := grpc.Dial(
    "user-service:9001",
    grpc.WithChainUnaryInterceptor(
        ratelimit.UnaryClientInterceptor(limiter),
        brk.UnaryClientInterceptor(),
    ),
)
```

## 常见问题

### Q: 熔断器和限流器有什么区别？

**A:**
- **限流器（RateLimit）**：控制请求速率，防止过载
- **熔断器（Breaker）**：检测故障，快速失败，避免级联故障

两者可以配合使用，提供更完善的保护。

### Q: 如何选择合适的 Timeout 值？

**A:**
- 短 Timeout（5-10s）：快速恢复，但可能频繁切换状态
- 长 Timeout（30-60s）：稳定性好，但恢复较慢
- 建议：根据下游服务的恢复时间设置，通常 30s 是一个合理的值

### Q: 为什么我的熔断器没有触发？

**A:** 检查以下几点：
1. 请求数是否达到 `MinimumRequests`
2. 失败率是否超过 `FailureRatio`
3. 是否正确注入了拦截器
4. 查看日志确认熔断器是否正常工作

## 参考资料

- [gobreaker 库文档](https://github.com/sony/gobreaker)
- [Circuit Breaker 模式](https://martinfowler.com/bliki/CircuitBreaker.html)
- [微服务容错模式](https://docs.microsoft.com/en-us/azure/architecture/patterns/circuit-breaker)

## 许可证

本组件遵循 Genesis 项目的许可证。
