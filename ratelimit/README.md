# ratelimit

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/ratelimit.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/ratelimit)

`ratelimit` 是 Genesis 的治理层（L3）组件，提供两种限流能力：

- `standalone`：基于内存的进程内限流
- `distributed`：基于 Redis 的分布式限流

它解决的不是“所有限流问题”，而是最常见的接口保护问题：给某个业务键配置 `Rate/Burst`，然后在请求进入业务逻辑之前做一次非阻塞检查。

如果你需要的是：

- HTTP / gRPC 请求入口保护；
- 按 IP、用户、方法名、路径等维度限流；
- 单机或 Redis 共享限流；
- 在限流器异常时显式选择 `fail_open` 或 `fail_closed`；

那么当前 `ratelimit` 是合适的。

如果你需要的是：

- 多种分布式限流算法切换；
- 复杂配额体系；
- 精细的剩余令牌、重试时间和窗口统计接口；
- 分布式 `Wait` 或排队语义；

那么当前组件不覆盖这些能力。

## 快速开始

### 1. 单机模式

```go
limiter, err := ratelimit.New(&ratelimit.Config{
	Driver: ratelimit.DriverStandalone,
	Standalone: &ratelimit.StandaloneConfig{
		CleanupInterval: time.Minute,
		IdleTimeout:     5 * time.Minute,
	},
}, ratelimit.WithLogger(logger))
if err != nil {
	panic(err)
}
defer limiter.Close()

allowed, err := limiter.Allow(ctx, "user:123", ratelimit.Limit{
	Rate:  10,
	Burst: 20,
})
if err != nil {
	panic(err)
}
if !allowed {
	return
}
```

### 2. 分布式模式

```go
redisConn, err := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
if err != nil {
	panic(err)
}
defer redisConn.Close()

limiter, err := ratelimit.New(&ratelimit.Config{
	Driver: ratelimit.DriverDistributed,
	Distributed: &ratelimit.DistributedConfig{
		Prefix: "myapp:ratelimit:",
	},
}, ratelimit.WithRedisConnector(redisConn), ratelimit.WithLogger(logger))
if err != nil {
	panic(err)
}

allowed, err := limiter.Allow(ctx, "user:123", ratelimit.Limit{
	Rate:  100,
	Burst: 200,
})
```

分布式模式当前仍然是令牌桶语义，但有两个实现细节需要知道：

- Redis 键会把 `key + rate + burst` 一起编码进去，避免同一个业务键在不同规则下互相串扰。
- 脚本使用 Redis `TIME` 作为统一时钟，而不是各节点本地时间。

## Gin 集成

```go
r := gin.New()
r.Use(ratelimit.GinMiddleware(limiter, &ratelimit.GinMiddlewareOptions{
	KeyFunc: func(c *gin.Context) string {
		return c.ClientIP()
	},
	LimitFunc: func(c *gin.Context) ratelimit.Limit {
		return ratelimit.Limit{Rate: 100, Burst: 200}
	},
}))
```

默认行为是：

- `KeyFunc` 留空时使用 `ClientIP()`
- `LimitFunc` 留空时视为无效规则并放行
- 限流器内部异常时采用 `fail_open`

如果你希望限流器异常时直接拒绝请求，可以切换到 `fail_closed`：

```go
r.Use(ratelimit.GinMiddleware(limiter, &ratelimit.GinMiddlewareOptions{
	ErrorPolicy: ratelimit.ErrorPolicyFailClosed,
	KeyFunc: func(c *gin.Context) string {
		return c.ClientIP()
	},
	LimitFunc: func(c *gin.Context) ratelimit.Limit {
		return ratelimit.Limit{Rate: 100, Burst: 200}
	},
}))
```

## gRPC 集成

最简单的接法是使用默认 `fail_open` 的拦截器：

```go
server := grpc.NewServer(
	grpc.ChainUnaryInterceptor(
		ratelimit.UnaryServerInterceptor(limiter, nil, func(ctx context.Context, fullMethod string) ratelimit.Limit {
			return ratelimit.Limit{Rate: 100, Burst: 200}
		}),
	),
)
```

如果你需要显式错误策略，可以使用带 `Options` 的版本：

```go
server := grpc.NewServer(
	grpc.ChainUnaryInterceptor(
		ratelimit.UnaryServerInterceptorWithOptions(
			limiter,
			nil,
			func(ctx context.Context, fullMethod string) ratelimit.Limit {
				return ratelimit.Limit{Rate: 100, Burst: 200}
			},
			&ratelimit.GRPCInterceptorOptions{
				ErrorPolicy: ratelimit.ErrorPolicyFailClosed,
				Logger:      logger,
			},
		),
	),
)
```

流式拦截器当前是 **per-stream** 限流，也就是只在流建立时检查一次，不对流中的每条消息逐条限流。

## 使用边界

- `Allow` / `AllowN` 是核心能力，适用于两种驱动。
- `Wait` 只适用于单机模式；分布式模式返回 `ErrNotSupported`。
- 当前分布式实现只有 Redis 令牌桶，没有滑动窗口、漏桶等可切换算法。
- 中间件和拦截器默认 `fail_open`，这是为了把限流器故障和业务故障隔离开；如果你的场景更重保护而不是可用性，应显式改成 `fail_closed`。
- `MetricAllowTotal`、`MetricErrors` 等更细粒度指标常量已经定义，但当前实现主要记录的是允许/拒绝计数。

更完整的设计背景、分布式语义和工程取舍见：[Genesis ratelimit：请求入口限流组件的设计与取舍](../docs/genesis-ratelimit-blog.md)
