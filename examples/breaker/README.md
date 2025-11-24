# Circuit Breaker 示例

这个示例展示了 Genesis 框架中 breaker 熔断器组件的使用方法。

## 实现说明

本组件基于成熟的 [gobreaker](https://github.com/sony/gobreaker) 库实现，该库由 Sony 开发并维护，具有良好的稳定性和性能。我们在保持框架一致性的基础上，封装了 gobreaker 的核心功能。

## 功能特性

- **三态状态机**: Closed (正常) -> Open (熔断) -> HalfOpen (半开) -> Closed
- **基于 gobreaker**: 使用成熟的第三方库，稳定可靠
- **服务级粒度**: 按目标服务名独立熔断，互不影响
- **灵活降级**: 支持快速失败和自定义 fallback 两种策略
- **gRPC 集成**: 提供 UnaryClientInterceptor，无侵入集成
- **状态监控**: 提供详细的状态变化日志

## 运行示例

### 基础示例

```bash
go run main.go
```

### gRPC 集成示例

```bash
go run grpc_example.go
```

## 核心 API

### 创建熔断器

```go
import (
    "github.com/ceyewan/genesis/pkg/breaker"
    "github.com/ceyewan/genesis/pkg/breaker/types"
)

b, err := breaker.New(&types.Config{
    Default: types.Policy{
        FailureThreshold:    0.5,  // 50% 失败率触发熔断
        WindowSize:          100,  // 滑动窗口大小
        MinRequests:         10,   // 最小请求数
        OpenTimeout:         30 * time.Second, // 熔断持续时间
        HalfOpenMaxRequests: 3,    // 半开状态最大探测请求数
    },
    Services: map[string]types.Policy{
        "user.v1.UserService": {
            FailureThreshold: 0.3, // 用户服务更敏感
            WindowSize:       50,
        },
    },
}, breaker.WithLogger(logger))
```

### 手动使用

```go
// 普通执行
err := b.Execute(ctx, "user.v1.UserService", func() error {
    return callUserService()
})

// 带降级逻辑
err := b.ExecuteWithFallback(ctx, "user.v1.UserService",
    func() error {
        return callUserService()
    },
    func(err error) error {
        // 返回缓存数据
        return getCachedUserData()
    },
)
```

### gRPC 集成

```go
conn, err := grpc.Dial(
    "localhost:9090",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithUnaryInterceptor(
        adapter.UnaryClientInterceptor(b,
            adapter.WithFallbackHandler(func(ctx context.Context, method string, err error) error {
                // 全局降级处理
                return getFallbackData(method)
            }),
        ),
    ),
)
```

## 状态监控

```go
// 获取熔断器状态
state := b.State("user.v1.UserService")
fmt.Printf("Current state: %s\n", state) // closed | open | half_open

// 手动重置
b.Reset("user.v1.UserService")
```

## 配置说明

| 参数 | 说明 | 默认值 |
|------|------|--------|
| FailureThreshold | 失败率阈值 (0.0-1.0) | 0.5 |
| WindowSize | 滑动窗口大小 | 100 |
| MinRequests | 最小请求数 | 10 |
| OpenTimeout | 熔断持续时间 | 30s |
| HalfOpenMaxRequests | 半开状态最大探测数 | 3 |
| CountTimeout | 是否将超时计入失败 | false |

## 错误处理

熔断器会返回以下特定错误：

- `ErrOpenState`: 熔断器处于 Open 状态，请求被拒绝
- `ErrTooManyRequests`: 半开状态下探测请求数已达上限
- `ErrInvalidConfig`: 配置无效

## 最佳实践

1. **合理设置阈值**: 根据服务重要性调整失败率阈值
2. **窗口大小**: 根据请求频率选择合适的窗口大小
3. **超时时间**: 根据服务恢复时间设置合理的 OpenTimeout
4. **降级策略**: 为关键服务提供有意义的降级数据
5. **监控告警**: 结合 Metrics 监控熔断器状态变化
