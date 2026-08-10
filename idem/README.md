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
},
	idem.WithRedisConnector(redisConn),
	idem.WithLogger(logger),
	idem.WithMaxKeyBytes(256),
	idem.WithMaxResultBytes(1<<20),
)
if err != nil {
	return err
}
defer idemComp.Close()

result, err := idemComp.Execute(ctx, "order:create:req-123", func(ctx context.Context) (any, error) {
	return map[string]any{"order_id": "123"}, nil
})
```

`Execute` 会把首次成功执行与缓存命中都统一成同一套 JSON 编解码后的结果形态，因此返回值适合按通用 JSON 结构读取，而不是依赖第一次执行时的原始 Go 类型。

组件由创建者负责调用 `Close`，且可以并发重复关闭。Memory 后端的 `Close` 会停止过期锁和结果的后台清理；Redis 后端只借用注入的 connector，`Close` 不会关闭 Redis 连接。

## 核心能力

`Execute` 适合业务层直接调用。它会先查结果缓存，未命中时抢锁执行，成功后写入缓存并释放锁；失败则不缓存，后续允许重试。

结果查询与抢锁之间存在一个容易忽略的并发窗口：前一个执行者可能恰好在一次查询返回 miss 之后提交结果并释放锁。内建 Redis 后端用 Lua、Memory 后端用同一把 mutex，原子完成“确认没有结果再抢锁”；公共执行链路还会在抢锁后再次查询结果，兼容只能提供基础 `Store` 接口的第三方实现。`Execute` 和 `Consume` 都不会因为这个窗口重复执行业务函数。

`Consume` 适合消息消费去重。它只关心“是否已处理”，不会返回业务结果；如果发现同 key 已完成，直接返回 `executed=false`。

`GinMiddleware` 和 `UnaryServerInterceptor` 则把这套逻辑分别接到 HTTP 和 gRPC 服务端入口。默认情况下，Gin 只缓存 `2xx` 响应，gRPC 只缓存成功的 `proto.Message` 响应。这两个策略现在都可以通过 option 显式调整。

## 资源上限

`idem` 默认对客户端 key、缓存结果、带 key 的 HTTP 请求体和内置 Memory 后端基数设置硬边界。这些边界使用 additive option 配置，不增加 `Config` 的序列化字段：

| Option | 默认值 | 作用范围 |
| :-- | --: | :-- |
| `WithMaxKeyBytes` | 256 B | `Execute`、`Consume`、HTTP header 和 gRPC metadata 中的原始 key；按 UTF-8 字节数计算。 |
| `WithMaxResultBytes` | 1 MiB | 实际写入 Store 的序列化结果；HTTP 响应体捕获也不会超过这个逻辑上限。 |
| `WithHTTPMaxRequestBytes` | 1 MiB | 仅带幂等 key 的 HTTP 请求体；无 key 请求直接放行。 |
| `WithMemoryMaxEntries` | 10000 | 内置 Memory 后端的逻辑 key 数；不影响 Redis 或 `WithStore` 注入的实现。 |

以上 option 的非正值会被忽略，继续使用安全默认值。key 超限在业务执行前失败：直接入口返回可用 `errors.Is` 匹配的 `ErrKeyTooLong`，HTTP 返回 `400`，gRPC 返回 `InvalidArgument`。带 key 的 HTTP 请求体超限返回 `413`，handler 不会执行。

Memory 后端达到容量后，新 key 返回 `ErrStoreCapacity`；已有 key 仍可读取或更新，过期条目会在同一临界区内先清理再决定是否接纳新 key。HTTP 将容量错误映射为 `503`，gRPC 映射为 `ResourceExhausted`，并且都发生在 handler 前。自定义限容 Store 必须在 `Lock` 成功时为对应 token 的 `SetResult` 预留提交容量，不能等业务成功后才拒绝 lock 到 result 的转换。

## 配置说明

| 字段 | 类型 | 默认值 | 说明 |
| :-- | :-- | :-- | :-- |
| `Driver` | `DriverType` | `redis` | 后端类型，支持 `redis` 和 `memory`。 |
| `Prefix` | `string` | `idem:` | 存储 key 前缀。 |
| `DefaultTTL` | `time.Duration` | `24h` | 成功结果的缓存有效期。 |
| `LockTTL` | `time.Duration` | `30s` | 执行阶段锁的有效期。 |
| `WaitTimeout` | `time.Duration` | `0` | 等待结果或锁的超时；`0` 表示仅受上层 ctx 控制。 |
| `WaitInterval` | `time.Duration` | `50ms` | 等待轮询间隔。 |

负数配置现在会被显式拒绝，而不是静默回退默认值。所有非 nil 配置错误都可以用 `errors.Is(err, idem.ErrInvalidConfig)` 分类；nil config 同时匹配更具体的 `ErrConfigNil` 和 `ErrInvalidConfig`。Redis connector 缺失或尚未 `Connect` 时同时匹配 `ErrConnectorNil` 与 `connector.ErrClientNil`，调用方不需要解析错误文本。

## 缓存策略

HTTP 中间件默认缓存 `2xx` 响应。如果你希望把某些 `4xx` 也视为可复用结果，可以通过 `WithHTTPStatusCacheFunc` 显式指定：

```go
middleware := idemComp.GinMiddleware(
	idem.WithHTTPStatusCacheFunc(func(status int) bool {
		return status == http.StatusConflict
	}),
)
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

成功结果或响应序列化后超过 `WithMaxResultBytes` 时，当前业务结果仍会完整返回，但不会写入缓存。`Execute` 仍按既有 JSON 规范化语义返回；HTTP/gRPC 仍把完整响应发送给调用方，HTTP 捕获在达到上限后只继续透传而不继续保留响应体。这个分支等价于本次成功主动退出结果复用：执行期间仍有锁保护，但完成解锁后，后续调用以及已经等待的同 key 调用都可能再次执行。若业务不能接受重放，就必须让成功结果保持在上限内，或在业务存储层使用唯一约束/CAS；“不保存任何结果”与“后续仍能复用同一次成功”无法同时成立。

## 多租户与主体隔离

默认 key 会绑定协议类型、HTTP route 或 gRPC full method，以及客户端提供的幂等 key；fingerprint 会绑定规范化请求内容。组件无法自行判断认证系统里的 tenant 或 principal，因此多租户服务必须显式提供可信身份作用域。仅调用 `WithHeaderKey` 或 `WithMetadataKey` 只是更换客户端 key 的字段名，不能建立身份隔离。

HTTP 入口应让认证中间件先把已经验证的身份写入 Gin context，再配置 `WithHTTPIdentityScopeFunc`：

```go
router.Use(authMiddleware)
router.Use(idemComp.GinMiddleware(
	idem.WithHTTPIdentityScopeFunc(func(c *gin.Context) (string, error) {
		tenantID := c.GetString("tenant_id")
		principalID := c.GetString("principal_id")
		if tenantID == "" || principalID == "" {
			return "", errors.New("verified identity is missing")
		}
		return tenantID + "\x00" + principalID, nil
	}),
))
```

gRPC 入口使用认证拦截器写入的 context：

```go
server := grpc.NewServer(grpc.ChainUnaryInterceptor(
	authInterceptor,
	idemComp.UnaryServerInterceptor(
		idem.WithGRPCIdentityScopeFunc(func(ctx context.Context) (string, error) {
			return verifiedIdentityScope(ctx)
		}),
	),
))
```

scope 会同时经过哈希绑定到 storage key 和 fingerprint，不会以明文身份出现在 Redis key 或组件日志中。不同身份即使复用完全相同的客户端 key 和请求内容，也不会共享处理锁或缓存响应。配置了回调后，空 scope 或回调错误会 fail closed（HTTP `401`、gRPC `Unauthenticated`），不会悄悄退回未隔离模式。不要直接把未经认证的 header 或 metadata 当作可信 scope。

## 续期与异常边界

对于耗时较长的执行，`idem` 会在锁生命周期过半时尝试自动续期，避免执行过程中锁提前过期。如果续期失败，组件现在会把它视为真实错误，而不是只记 warning。对 `Execute` 和 `Consume` 这类直接调用场景，这会阻止成功结果被继续缓存，降低“锁已经丢了但本地还在提交结果”的风险。

对 HTTP/gRPC 中间件场景，续期失败同样会阻止结果进入缓存，但如果业务 handler 已经把响应写给客户端，组件无法回滚已经发送出去的响应。这是应用层幂等组件的天然边界。

## 推荐实践

最重要的设计点仍然是 **key 设计**。幂等 key 必须和业务操作绑定，至少要能区分“同一个用户的同一次提交”和“两个不同请求”。常见做法是 `source + business_id + request_id`；在认证入口还应通过 identity scope 绑定可信的 tenant/principal，而不是让客户端自行声明身份前缀。

第二个关键点是 **把返回值当作 JSON 友好数据读取**。如果你在 `Execute` 的返回值上依赖具体 Go 结构体类型断言，那么第一次执行和缓存命中都很容易出问题。对于强类型恢复需求，更合适的方向通常是业务层自带编解码，或者后续引入显式 codec。
