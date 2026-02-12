# Genesis Ratelimit：分布式限流的设计与实现

Genesis `ratelimit` 是治理层（L3）的核心组件，提供单机和分布式两种限流模式。它统一了 `Limiter` 接口，旨在通过精细的流量控制，保护微服务免受突发流量和恶意攻击的影响。本文将深入探讨其背后的算法原理、Redis Lua 脚本实现细节以及在实际工程中的应用。

## 0. 摘要

- **统一抽象**：提供 `Limiter` 接口，屏蔽单机（内存）与分布式（Redis）的实现差异
- **算法核心**：基于**令牌桶（Token Bucket）**算法，兼顾平滑限流与突发容忍
- **分布式实现**：采用 **GCRA（Generic Cell Rate Algorithm）** 的变体，通过 Lua 脚本在 Redis 中实现原子操作，无需显式维护令牌计数
- **降级策略**：中间件层默认采用"失败放行"策略，优先保证业务可用性
- **多场景支持**：内置 Gin 中间件和 gRPC 拦截器，支持 Per-Stream 限流

---

## 1. 背景：限流算法的选择与取舍

在设计限流组件时，我们通常面临三种主流算法的选择：

### 1.1 滑动窗口 (Sliding Window)

滑动窗口通过记录时间窗口内的请求时间戳来统计数量。它的优点是平滑，解决了固定窗口的临界突变问题。但其缺点也很明显，需要存储所有请求的时间戳，空间复杂度随请求量线性增长，在分布式环境下同步成本极高。

### 1.2 漏桶 (Leaky Bucket)

漏桶算法将请求视为水流入桶中，并以恒定速率流出。桶满则溢出（拒绝）。它的优点是输出速率绝对恒定，适合整流。但其缺点是**无法处理突发流量**（Burst）。即使系统空闲已久，新请求也只能按恒定速率被处理，这对于追求低延迟的微服务调用并不友好。

### 1.3 令牌桶 (Token Bucket) - Genesis 的选择

令牌桶算法以固定速率向桶中放入令牌，桶满则丢弃。请求消耗令牌，无令牌则拒绝。它具备两个核心优势：首先是**支持突发**，如果桶内有存量令牌，允许短时间的并发激增；其次是**易于实现**，无论是单机还是分布式，都有成熟的轻量级实现方案。

Genesis 最终选择了**令牌桶算法**，因为它最符合微服务场景的需求：既要限制平均速率保护系统，又要容忍正常的短时业务抖动。

---

## 2. 核心设计：Lua 脚本深度解析

在分布式模式下，Genesis 使用 Redis Lua 脚本来实现令牌桶。与直观的"定时器往 Redis 里加数字"不同，我们采用了一种更高效的**惰性计算**方式——基于时间戳的 GCRA 变体。这种方式不需要后台线程定期填充令牌，而是完全基于请求到达的时间点进行计算。

### 2.1 算法原理：时间即令牌

算法的核心思想是将"令牌数"转换为"时间成本"。我们定义 `interval_per_token` 为生成一个令牌所需的时间（即 `1 / rate`）。每个请求的到来，本质上是要求系统支付相应的时间成本。

我们引入一个虚拟时钟 `last_refreshed`，记录上一次令牌发放完毕的时间点。当新请求到达时，我们比较当前时间 `now` 和 `last_refreshed`。如果 `now > last_refreshed`，说明系统已经空闲了一段时间，这段时间足够生成新的令牌，因此我们将虚拟时钟重置为 `now`，相当于桶被填满了。

接着，我们计算处理当前请求后的新时间点 `new_refreshed`。如果这个新时间点超过了当前时间加上桶容量所代表的时间跨度（`allow_at_most`），则说明请求速率超过了系统的填充速率与桶容量之和，应当拒绝。否则，请求通过，并更新 `last_refreshed`。

这种逻辑完全避免了并发竞争，因为 Redis 单线程执行 Lua 脚本保证了读取和更新的原子性。

### 2.2 脚本逻辑实现

脚本接收速率（rate）、容量（capacity）、当前时间（now）和请求令牌数（requested）作为参数。

首先，计算基本参数。每个令牌的时间间隔 `interval` 为速率的倒数。桶填满所需时间 `fill_time` 为容量乘以间隔。

其次，获取 Redis 中存储的 `last_refreshed` 时间戳。如果键不存在，说明是首次访问，将其初始化为当前时间 `now`。

然后，计算下一次可用的基准时间 `next_available`。取 `last_refreshed` 和 `now` 中的较大值。这一步体现了令牌桶的填充逻辑：如果距离上次请求已经很久（`now` 很大），则之前的时间成本已被“抵消”，新的计算从当前时间开始。

接着，推算请求后的新时间点 `new_refreshed`，即基准时间加上本次请求所需的时间成本。

最后，进行判定。计算允许的最大时间边界 `allow_at_most`，即 `now + fill_time`。如果 `new_refreshed` 小于等于这个边界，说明桶内令牌足够，请求通过，并将 `new_refreshed` 写入 Redis，同时设置过期时间以自动清理闲置键。反之，如果超过边界，则拒绝请求。

### 2.3 关键代码摘要

```lua
-- 计算允许的最大时间边界（桶满时刻）
local fill_time = capacity * interval_per_token
local allow_at_most = now + fill_time

-- 计算理论上的新恢复时间
-- next_available_time 是 max(last_refreshed, now)
local new_refreshed = next_available_time + requested * interval_per_token

-- 判定
if new_refreshed <= allow_at_most then
  -- 允许通过：更新 Redis
  redis.call("SET", KEYS[1], new_refreshed, "EX", math.ceil(fill_time * 2))
  return 1
else
  -- 拒绝
  return 0
end
```

这种设计极其精简，只需要 Redis 存储一个浮点数（时间戳），不需要额外的计数器 key，空间利用率高，且过期策略自然处理了冷数据清理。

---

## 3. 实战落地

### 3.1 初始化

限流器的创建遵循 Genesis 的依赖注入模式。我们需要先初始化 Redis 连接器，再将其注入到分布式限流器中。

```go
// 1. 初始化 Redis 连接器 (L1)
rdb, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))

// 2. 初始化分布式限流器 (L3)
// 配置: 前缀 /genesis/ratelimit
limiter, err := ratelimit.NewDistributed(&ratelimit.DistributedConfig{
    Prefix: "/genesis/ratelimit",
}, rdb, logger, meter)
```

### 3.2 业务代码调用

最基础的用法是在业务逻辑中显式检查。这种方式适合对特定业务操作进行细粒度控制。

```go
func HandleRequest(ctx context.Context, userID string) error {
    // 定义规则：每秒 10 次，突发 20 次
    limit := ratelimit.Limit{
        Rate:  10,
        Burst: 20,
    }

    // 检查限流
    // key 建议组合业务前缀，如 "user_upload:{uid}"
    allowed, err := limiter.Allow(ctx, "user_upload:"+userID, limit)
    
    // 降级策略：基础设施错误时优先保业务
    if err != nil {
        logger.Error("ratelimit error, allowing", clog.Error(err))
        return nil 
    }

    if !allowed {
        return xerrors.New("too many requests")
    }

    // 业务逻辑...
    return nil
}
```

### 3.3 HTTP 中间件集成 (Gin)

Genesis 提供了开箱即用的 Gin 中间件，可以方便地挂载到路由组上。用户需要提供提取 Key（如 IP 或 UserID）和提取规则的函数。

```go
r := gin.New()

// 注册中间件
r.Use(ratelimit.GinMiddleware(limiter, 
    // Key 提取函数
    func(c *gin.Context) string {
        return c.ClientIP()
    },
    // 规则提取函数
    func(c *gin.Context) ratelimit.Limit {
        return ratelimit.Limit{Rate: 100, Burst: 200}
    },
))
```

### 3.4 gRPC 拦截器集成

对于 gRPC 服务，支持 Unary 和 Stream 两种拦截器。对于 Streaming RPC，拦截器仅在**建立连接时**进行一次限流检查，而不会对流中的每条消息进行检查。这是为了避免长连接中途突然中断，导致协议状态不一致，影响客户端体验。

```go
s := grpc.NewServer(
    grpc.UnaryInterceptor(ratelimit.UnaryServerInterceptor(limiter, 
        func(ctx context.Context, req any, info *grpc.UnaryServerInfo) (string, ratelimit.Limit) {
            // 按 FullMethod 限流
            return info.FullMethod, ratelimit.Limit{Rate: 500, Burst: 1000}
        },
    )),
)
```

---

## 4. 最佳实践与常见坑

### 4.1 Key 的设计

Key 的设计直接决定了限流的粒度。全局限流通常使用固定字符串（如 `global_api`），适合保护整个系统入口；用户级限流使用 `user:{uid}`，防止单用户滥用；IP 级限流使用 `ip:{ip_addr}`，防范爬虫。

需要注意的是**热点问题**。如果 Key 的基数非常大（如 IP 限流），在分布式模式下虽然 Redis 的内存占用是可控的（依赖过期时间），但高频的 Redis 访问可能带来网络和 CPU 压力。

### 4.2 降级策略

当 `limiter.Allow` 返回 error（例如 Redis 连接超时）时，**强烈建议放行**。因为限流组件是辅助性的治理工具，如果因为限流组件自身的故障导致核心业务全站不可用，是得不偿失的。我们在中间件实现中默认遵循了这一原则。

### 4.3 分布式 vs 单机

单机模式（`NewStandalone`）基于内存实现，性能极高（微秒级），适合 Sidecar 模式或单体应用，但无法控制集群总并发。分布式模式（`NewDistributed`）基于 Redis，能精准控制集群总流量，但多一次网络往返（RTT）。

在复杂系统中，推荐采用**多级限流**架构：在网关层使用分布式限流防范整体过载，在微服务节点内部使用单机限流保护自身资源（CPU/内存）。

---

## 5. 总结

Genesis `ratelimit` 组件通过统一的接口封装和精细的算法实现，解决了微服务架构中的流量治理问题。其核心亮点在于利用 Redis Lua 脚本实现了无锁的分布式令牌桶算法，兼具高性能与原子性。在实际应用中，合理的 Key 设计、稳健的降级策略以及多级限流的配合，是构建高可用系统的关键。
