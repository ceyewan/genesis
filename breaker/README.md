# breaker

`breaker` 是 Genesis 治理层的核心组件，提供了专注于 gRPC 客户端的故障隔离与自动恢复能力。它基于 [sony/gobreaker](https://github.com/sony/gobreaker) 实现，支持服务级粒度的熔断管理。

## 特性

- **服务级熔断**：按目标服务名（或自定义 Key）独立管理熔断状态。
- **gRPC 集成**：提供 `UnaryClientInterceptor`，无侵入式集成。
- **自动恢复**：支持半开状态（Half-Open）探测，自动从故障中恢复。
- **灵活降级**：支持快速失败或自定义降级逻辑（Fallback）。
- **可观测性**：集成 Genesis 标准日志（clog）。

## 安装

```bash
go get github.com/ceyewan/genesis/breaker
```

## 快速开始

### 1. 创建熔断器

```go
import (
    "github.com/ceyewan/genesis/breaker"
    "github.com/ceyewan/genesis/clog"
    "time"
)

// 配置熔断规则
cfg := &breaker.Config{
    MaxRequests:     5,                // 半开状态下允许的最大请求数
    Interval:        60 * time.Second, // 统计周期
    Timeout:         30 * time.Second, // 熔断打开状态持续时间
    FailureRatio:    0.6,              // 触发熔断的失败率阈值 (60%)
    MinimumRequests: 10,               // 触发熔断的最小请求数
}

// 创建实例
brk, err := breaker.New(cfg, breaker.WithLogger(logger))
```

### 2. 使用 gRPC 拦截器

```go
conn, err := grpc.NewClient(
    "localhost:9001",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
)
```

## 配置说明

### 核心配置 (Config)

| 字段              | 类型       | 说明                                | 默认值 |
| :---------------- | :--------- | :---------------------------------- | :----- |
| `MaxRequests`     | `uint32`   | 半开状态下允许通过的最大请求数      | 1      |
| `Interval`        | `duration` | 闭合状态下的统计周期 (0 表示不清空) | 0      |
| `Timeout`         | `duration` | 打开状态持续时间 (冷却时间)         | 60s    |
| `FailureRatio`    | `float64`  | 触发熔断的失败率阈值                | 0.6    |
| `MinimumRequests` | `uint32`   | 触发熔断的最小请求数                | 10     |

### 拦截器选项 (InterceptorOption)

- `WithKeyFunc(fn KeyFunc)`: 自定义熔断 Key 生成策略（默认使用 `cc.Target()`）。

```go
// 使用方法名作为熔断 Key
conn, err := grpc.NewClient(
    "etcd:///logic-service",
    grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
        breaker.WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
            return fullMethod // 如 "/pkg.Service/Method"
        }),
    )),
)
```

## 降级策略 (Fallback)

您可以为熔断器配置 `Fallback` 函数，当熔断器打开时执行自定义逻辑。

```go
fallback := func(ctx context.Context, key string, err error) error {
    // 返回缓存数据或 nil (表示降级成功)
    return nil
}

brk, _ := breaker.New(cfg, breaker.WithFallback(fallback))
```
