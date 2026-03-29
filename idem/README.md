# idem

`idem` 是 Genesis 业务层的结果复用型幂等组件，用来抑制同一请求、同一消息或同一 RPC 的重复成功提交。它的核心机制是“结果缓存 + 锁保护”：第一次执行成功后缓存结果，后续相同 key 直接复用；如果执行还在进行中，则通过锁避免并发穿透。

这里的语义边界需要先说清。`idem` 不是严格的 exactly-once 执行器，它更准确地说是“防重复成功提交”和“结果复用”组件。成功结果会被缓存，失败结果不会缓存；如果执行过程中锁丢失或存储异常，也不能承诺绝对的一次且仅一次。

## 适用场景

适合的场景包括：HTTP 接口幂等提交、gRPC 一元调用去重、消息消费去重，以及业务层显式控制的“只希望成功一次”的操作。

不太适合的场景包括：你需要强类型结果恢复、复杂流式响应缓存、严格的分布式事务语义，或者希望组件替你保证数据库层面的 exactly-once 提交。当前 `idem` 更适合做应用层幂等保护，而不是事务系统。

## 快速开始

```go
idemComp, err := idem.New(&idem.Config{
	Driver:     idem.DriverRedis,
	Prefix:     "myapp:idem:",
	DefaultTTL: 24 * time.Hour,
	LockTTL:    30 * time.Second,
}, idem.WithRedisConnector(redisConn), idem.WithLogger(logger))
if err != nil {
	return err
}

result, err := idemComp.Execute(ctx, "order:create:req-123", func(ctx context.Context) (any, error) {
	return map[string]any{"order_id": "123"}, nil
})
```

`Execute` 会把首次成功执行与缓存命中都统一成同一套 JSON 编解码后的结果形态，因此返回值适合按通用 JSON 结构读取，而不是依赖第一次执行时的原始 Go 类型。

## 核心能力

`Execute` 适合业务层直接调用。它会先查结果缓存，未命中时抢锁执行，成功后写入缓存并释放锁；失败则不缓存，后续允许重试。

`Consume` 适合消息消费去重。它只关心“是否已处理”，不会返回业务结果；如果发现同 key 已完成，直接返回 `executed=false`。

`GinMiddleware` 和 `UnaryServerInterceptor` 则把这套逻辑分别接到 HTTP 和 gRPC 服务端入口。默认情况下，Gin 只缓存 `2xx` 响应，gRPC 只缓存成功的 `proto.Message` 响应。这两个策略现在都可以通过 option 显式调整。

## 配置说明

| 字段 | 类型 | 默认值 | 说明 |
| :-- | :-- | :-- | :-- |
| `Driver` | `DriverType` | `redis` | 后端类型，支持 `redis` 和 `memory`。 |
| `Prefix` | `string` | `idem:` | 存储 key 前缀。 |
| `DefaultTTL` | `time.Duration` | `24h` | 成功结果的缓存有效期。 |
| `LockTTL` | `time.Duration` | `30s` | 执行阶段锁的有效期。 |
| `WaitTimeout` | `time.Duration` | `0` | 等待结果或锁的超时；`0` 表示仅受上层 ctx 控制。 |
| `WaitInterval` | `time.Duration` | `50ms` | 等待轮询间隔。 |

负数配置现在会被显式拒绝，而不是静默回退默认值。

## 缓存策略

HTTP 中间件默认缓存 `2xx` 响应。如果你希望把某些 `4xx` 也视为可复用结果，可以通过 `WithHTTPStatusCacheFunc` 显式指定：

```go
middleware := idemComp.GinMiddleware(
	idem.WithHTTPStatusCacheFunc(func(status int) bool {
		return status == http.StatusConflict
	}),
).(func(*gin.Context))
```

gRPC 拦截器默认缓存成功的 `proto.Message` 响应。你也可以通过 `WithGRPCResponseCacheFunc` 进一步缩小缓存范围：

```go
interceptor := idemComp.UnaryServerInterceptor(
	idem.WithGRPCResponseCacheFunc(func(msg proto.Message) bool {
		return msg.ProtoReflect().Descriptor().FullName() == "demo.OrderReply"
	}),
)
```

需要注意的是，当前 gRPC 幂等缓存仍然只支持 `proto.Message`。非 proto 成功结果不会被缓存。

## 续期与异常边界

对于耗时较长的执行，`idem` 会在锁生命周期过半时尝试自动续期，避免执行过程中锁提前过期。如果续期失败，组件现在会把它视为真实错误，而不是只记 warning。对 `Execute` 和 `Consume` 这类直接调用场景，这会阻止成功结果被继续缓存，降低“锁已经丢了但本地还在提交结果”的风险。

对 HTTP/gRPC 中间件场景，续期失败同样会阻止结果进入缓存，但如果业务 handler 已经把响应写给客户端，组件无法回滚已经发送出去的响应。这是应用层幂等组件的天然边界。

## 推荐实践

最重要的设计点仍然是 **key 设计**。幂等 key 必须和业务操作绑定，至少要能区分“同一个用户的同一次提交”和“两个不同请求”。常见做法是 `source + business_id + request_id`。

第二个关键点是 **把返回值当作 JSON 友好数据读取**。如果你在 `Execute` 的返回值上依赖具体 Go 结构体类型断言，那么第一次执行和缓存命中都很容易出问题。对于强类型恢复需求，更合适的方向通常是业务层自带编解码，或者后续引入显式 codec。
