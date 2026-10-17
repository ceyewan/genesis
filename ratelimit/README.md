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
		MaxKeys:        10000,
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
if err := redisConn.Connect(ctx); err != nil {
	panic(err)
}

limiter, err := ratelimit.New(&ratelimit.Config{
	Driver: ratelimit.DriverDistributed,
	Distributed: &ratelimit.DistributedConfig{
		Prefix: "myapp:ratelimit:",
	},
}, ratelimit.WithRedisConnector(redisConn), ratelimit.WithLogger(logger))
if err != nil {
	panic(err)
}
defer limiter.Close()

allowed, err := limiter.Allow(ctx, "user:123", ratelimit.Limit{
	Rate:  100,
	Burst: 200,
})
```

分布式模式使用普通令牌桶，只保存 `tokens`（剩余令牌）和 `last_us`（上次更新时间），
以 `tokens,last_us` 字符串存储：

```text
available = min(Burst, tokens + elapsed_seconds * Rate)
若 n <= available：允许，扣除 n；否则拒绝。
```

补充、判断和更新在一个 Redis Lua 脚本中原子执行。`n > Burst` 直接拒绝，不访问 Redis。
Redis 键仍编码 `key + rate + burst`，沿用原键名，不添加版本后缀。

精度与适用范围：

- 使用 Redis `TIME` 的整数微秒时间，先做时间差再补充令牌，不向大 Unix 秒时间戳累加微小间隔。
- 时钟为微秒粒度，令牌使用浮点近似计算。极高 Rate、小 Burst 或极大数值下会有粒度/舍入误差，
  不保证纳秒级放行或精确配额结算；需要严格计量时应使用专门的整数配额方案。
- Burst 超过 Lua 可精确表示的整数范围（`2^53-1`），或回满时间无法安全表示为 Redis 过期时间时，返回 `ErrInvalidLimit`。

读取到旧的单时间戳时，脚本会在同一次调用中折算剩余令牌并写回原字符串键，不无条件重置为满桶。
这只能沿用旧状态已有的精度，不能追回旧算法已经丢失的数值信息。
旧脚本不能识别新的双数值编码，须先停止旧实例再启用新实例，禁止新旧版本混写同一批键，见[迁移说明](../docs/migration.md)。

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
- `Limit.Rate` 必须是大于零的有限数，`Burst` 必须大于零；NaN 和正负无穷均返回 `ErrInvalidLimit`。
- 单机模式按 `key + Limit` 缓存桶，`MaxKeys` 默认 10000 且是硬上限；达到上限的新组合返回 `ErrKeyLimitExceeded`。不要把请求 ID 等无界高基数值直接作为 key。
- 当前分布式实现只有 Redis 令牌桶，没有滑动窗口、漏桶等可切换算法。
- 中间件和拦截器在未配置策略时默认 `fail_open`，这是为了把限流器故障和业务故障隔离开；如果你的场景更重保护而不是可用性，应显式改成 `fail_closed`。显式但未知的策略值按 `fail_closed` 处理，避免拼写错误静默关闭硬保护。
- Gin 的真实限流拒绝返回 `429 Too Many Requests`；限流器自身故障且采用 `fail_closed` 时返回 `503 Service Unavailable`，与 gRPC 的 `Unavailable` 语义一致。
- 当前实现记录 `MetricAllowed`（`ratelimit_allowed_total`）、`MetricDenied`（`ratelimit_denied_total`）和 `MetricErrors`（`ratelimit_errors_total`），并只以低基数 `LabelMode` 区分 standalone / distributed。容量拒绝、后端错误和其他操作错误计入 `MetricErrors`。

当前行为查 `go doc -all ./ratelimit`；策略选择的理由见[服务治理设计](../docs/design/service-governance.md)。
