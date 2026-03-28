# breaker

`breaker` 是 Genesis 治理层的熔断组件，面向 gRPC 客户端调用场景。它基于 `sony/gobreaker` 提供按 key 隔离的熔断状态机，用于在下游出现系统性异常时快速失败，并在冷却后通过半开探测自动恢复。

它的边界也很明确。`breaker` 负责故障隔离，不负责重试、不负责超时控制，也不负责生成业务层替代结果。组件内置了 gRPC 错误分类逻辑，默认会把 `Unavailable`、`Internal`、`ResourceExhausted` 一类系统性错误计入熔断统计，而把 `InvalidArgument`、`NotFound` 等明显业务错误排除在外。

## 适用场景

适合的场景是：你已经有稳定的 gRPC 客户端调用链，希望在下游不稳定时快速止损，并且希望按服务或方法维度隔离故障。它尤其适合和超时、有限重试一起使用。

不太适合的场景是：你需要返回缓存数据这类“替代结果”，或者你需要按 HTTP、数据库、消息队列等其他协议分别定义失败口径。当前 `breaker` 的强项是 gRPC 客户端拦截，而不是通用结果降级框架。

## 快速开始

```go
cfg := &breaker.Config{
	MaxRequests:     1,
	Interval:        30 * time.Second,
	Timeout:         10 * time.Second,
	FailureRatio:    0.6,
	MinimumRequests: 10,
}

brk, err := breaker.New(cfg, breaker.WithLogger(logger))
if err != nil {
	return err
}

conn, err := grpc.NewClient(
	"etcd:///logic-service",
	grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor()),
)
```

默认情况下，熔断 key 是 `cc.Target()`，也就是服务级粒度。如果你希望某个高错误率方法不要拖垮整个目标服务，可以通过 `WithKeyFunc` 改成方法级 key。

```go
conn, err := grpc.NewClient(
	"etcd:///logic-service",
	grpc.WithUnaryInterceptor(brk.UnaryClientInterceptor(
		breaker.WithKeyFunc(func(ctx context.Context, fullMethod string, cc *grpc.ClientConn) string {
			return fullMethod
		}),
	)),
)
```

## 配置说明

| 字段 | 类型 | 默认值 | 说明 |
| :-- | :-- | :-- | :-- |
| `MaxRequests` | `uint32` | `1` | 半开状态允许通过的最大探测请求数。 |
| `Interval` | `time.Duration` | `0` | 闭合状态下统计计数的重置周期，`0` 表示不自动重置。 |
| `Timeout` | `time.Duration` | `60s` | 打开状态持续时间，到期后进入半开状态。 |
| `FailureRatio` | `float64` | `0.6` | 熔断触发失败率阈值，必须在 `(0, 1]` 内。 |
| `MinimumRequests` | `uint32` | `10` | 触发熔断前所需的最小采样请求数。 |

`breaker.New` 会对配置做基础校验。当前会拒绝负数 `Interval`、负数 `Timeout` 以及不在 `(0, 1]` 范围内的 `FailureRatio`。

## Fallback 的真实语义

`WithFallback` 当前更准确的语义是“拒绝处理函数”，而不是“结果降级函数”。

它只会在 breaker 已经拒绝执行请求时触发，包括两类情况：打开状态直接拒绝，以及半开状态下探测请求数超过 `MaxRequests`。如果 fallback 返回 `nil`，表示吞掉本次拒绝；如果返回一个错误，该错误会继续向上返回。

这意味着它适合做：

- 统一改写拒绝错误
- 记录额外日志或埋点
- 在某些场景下把拒绝视为可接受结果

它不适合做：

- 返回缓存对象
- 构造通用的 gRPC 响应体
- 充当业务层降级结果工厂

## 推荐实践

最关键的两个调节点不是 `Timeout` 和 `FailureRatio`，而是 **key 粒度** 与 **失败口径**。如果你把多个差异很大的方法放在同一个 key 下，即使 gRPC 业务错误已经被分类排除，少数高失败率方法仍然可能拖累整个服务的 breaker 状态。

推荐做法是先用服务级 key 作为起点，再根据真实流量与错误分布决定是否拆到方法级。对参数校验失败、资源不存在这类稳定业务错误，不需要指望 breaker 来吸收，它本来就不该参与熔断统计。
