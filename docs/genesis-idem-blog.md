# Genesis Idem：分布式幂等的设计与实现

Genesis `idem` 是业务层（L2）的幂等组件，用来解决“同一请求/消息被重复提交”带来的副作用问题。它提供统一接口覆盖三类入口：手动调用、Gin 中间件、gRPC 一元拦截器。

---

## 0. 摘要

- **统一抽象**：提供 `Execute` / `Consume` 接口，屏蔽底层锁与存储细节
- **双驱动支持**：支持 `redis`（分布式）与 `memory`（单机）两种后端
- **结果缓存**：核心流程为“读缓存 -> 抢锁 -> 执行 -> 写缓存”，确保结果一致性
- **并发控制**：内置分布式锁与续期（Renew）机制，防止并发穿透与长任务锁过期
- **多端集成**：开箱即用的 Gin 中间件与 gRPC 拦截器，自动处理幂等 Header/Metadata

---

## 1. 背景：把幂等从“业务约定”变成“标准能力”

在分布式系统中，网络抖动导致客户端重试是常态。如果没有幂等机制，一次扣款请求的重试可能导致用户被扣两次钱。传统的做法是在业务代码中手动查询数据库、插入去重表或使用 Redis SETNX，代码侵入性强且容易出错。

Genesis `idem` 旨在提供一套标准的、可配置的幂等解决方案，将复杂的并发控制和结果缓存逻辑封装在组件内部，业务开发者只需关注核心逻辑。

`Idempotency` 接口对外提供四个入口：

- `Execute(ctx, key, fn)`：通用幂等执行，返回结果，适合业务逻辑手动调用
- `Consume(ctx, key, ttl, fn)`：消息消费去重，只关心“是否执行”，适合 MQ 消费者
- `GinMiddleware(opts...)`：HTTP 请求幂等，通过 Header 传递幂等键
- `UnaryServerInterceptor(opts...)`：gRPC 一元调用幂等，通过 Metadata 传递幂等键

---

## 2. 核心设计：结果优先 + 锁保护

### 2.1 `Execute` 的状态机

对同一 `key`，`Execute` 的主流程遵循“结果优先”原则，以最大程度减少重复计算：

1.  **查缓存**：首先尝试读取该 Key 对应的执行结果。如果命中，直接返回缓存结果（Success）。
2.  **抢锁**：如果未命中缓存，尝试获取分布式锁。
    - **抢锁成功**：执行业务函数 `fn`。
    - **抢锁失败**：说明有并发请求正在执行。此时不会立即失败，而是进入轮询等待状态，定期检查结果缓存或尝试重新抢锁。
3.  **写结果**：
    - `fn` 执行成功：将结果序列化并写入存储，同时释放锁。
    - `fn` 执行失败：不缓存结果，直接释放锁，允许后续重试。

这意味着：在并发压力下，通常只有一个请求真正执行业务逻辑，其它请求等到结果后直接复用，从而保证了系统的高效与一致性。

### 2.2 `Consume` 的差异化语义

`Consume` 专为消息队列消费场景设计，它不关心返回值，只关心“是否已处理”。

- **已处理**：返回 `executed=false, nil`，业务层直接 ACK 消息。
- **处理中**（抢锁失败）：返回 `executed=false, ErrConcurrentRequest`，业务层可选择 NACK 稍后重试，或丢弃（取决于业务对顺序性的要求）。
- **未处理**（抢锁成功）：执行 `fn`，成功后标记 Key 为已完成，返回 `executed=true, nil`。

### 2.3 锁续期（Renew）

为了防止长耗时任务执行期间锁过期导致并发穿透，组件内置了**自动看门狗**机制。当任务执行时间超过 `LockTTL` 的一半时，后台协程会自动延长锁的有效期，直到任务完成或明确失败。

---

## 3. 存储抽象与双驱动实现

`idem` 定义了 `Store` 接口来解耦上层逻辑与底层存储：

```go
type Store interface {
    Lock(ctx context.Context, key string, ttl time.Duration) (string, bool, error)
    Unlock(ctx context.Context, key string, token string) error
    SetResult(ctx context.Context, key string, result []byte, ttl time.Duration, token string) error
    GetResult(ctx context.Context, key string) ([]byte, bool, error)
}
```

### 3.1 Redis 驱动（分布式）

适用于生产环境。它使用两个 Key：

- `prefix + key + ":lock"`：分布式锁，值是随机 Token。
- `prefix + key + ":result"`：执行结果。

关键操作如 `Unlock` 和 `SetResult` 均使用 **Lua 脚本** 保证原子性，防止误删他人的锁。

### 3.2 Memory 驱动（单机）

适用于本地开发、单测或单体应用。它基于 `sync.Map` 实现，性能极高但不支持跨进程幂等。

---

## 4. 实战落地

### 4.1 初始化

```go
// 1. 初始化 Redis 连接器
rdb, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))

// 2. 初始化 Idem 组件
idempotency, _ := idem.New(&idem.Config{
    Driver:     idem.DriverRedis,
    Prefix:     "order:idem:",
    DefaultTTL: 24 * time.Hour, // 结果保留 24 小时
    LockTTL:    30 * time.Second, // 锁默认 30 秒
}, idem.WithRedisConnector(rdb), idem.WithLogger(logger))
```

### 4.2 手动调用 (Execute)

```go
// 定义业务逻辑
createOrder := func(ctx context.Context) (interface{}, error) {
    // ... 创建订单 ...
    return &Order{ID: "123"}, nil
}

// 执行幂等操作
// key: "create_order:{user_id}:{request_id}"
result, err := idempotency.Execute(ctx, "create_order:1001:req_abc", createOrder)
if err != nil {
    return err
}

order := result.(*Order)
```

### 4.3 Gin 中间件

```go
r := gin.New()

// 注册中间件
// 客户端需传递 Header: X-Idempotency-Key: <unique-key>
r.POST("/orders", idempotency.GinMiddleware(), func(c *gin.Context) {
    // ... 业务逻辑 ...
    c.JSON(200, gin.H{"status": "ok"})
})
```

**注意**：中间件默认只缓存 2xx 响应。如果业务逻辑返回 4xx/5xx，不会被缓存，允许客户端修正后重试。

### 4.4 gRPC 拦截器

```go
s := grpc.NewServer(
    // 注册一元拦截器
    // 客户端需传递 Metadata: x-idem-key: <unique-key>
    grpc.UnaryInterceptor(idempotency.UnaryServerInterceptor()),
)
```

**注意**：gRPC 拦截器仅支持 `proto.Message` 类型的响应缓存，且仅缓存执行成功的请求。

---

## 5. 最佳实践与常见坑

### 5.1 幂等键的设计

幂等键（Idempotency Key）的设计至关重要，必须保证**全局唯一**且与**业务操作绑定**。

- **好的设计**：`source + 业务ID + 操作类型`，例如 `app:order:create:req_12345`。
- **坏的设计**：只使用 UUID（无法与业务关联）、粒度过粗（导致不同用户的请求冲突）。

### 5.2 TTL 的选择

- **DefaultTTL（结果缓存时间）**：应覆盖客户端可能发起重试的最长周期。例如，如果客户端只在 1 分钟内重试，TTL 设为 1 小时足矣；如果涉及隔天对账，TTL 可能需要设为 24 小时甚至更长。
- **LockTTL（锁时间）**：应略大于业务逻辑的 P99 耗时。虽然有自动续期机制，但合理的初始值能减少不必要的网络开销。

### 5.3 错误处理

在使用 `Execute` 时，需要注意区分错误的类型：

- `ErrConcurrentRequest`：表示并发请求且等待超时。此时业务应根据情况决定是报错还是稍后重试。
- 业务逻辑错误：如果 `fn` 返回 error，`idem` 不会缓存结果。这意味着客户端重试时，`fn` 会被再次执行。这是符合预期的，因为失败的操作通常不具备幂等性约束（除非是永久性失败）。

---

## 6. 总结

Genesis `idem` 组件通过标准化接口和可靠的存储实现，将幂等性从复杂的业务逻辑中剥离出来。无论是简单的 API 去重，还是复杂的分布式消息处理，它都能提供一致、可靠的保障。正确使用 `idem`，能显著提升系统的健壮性和数据一致性。
