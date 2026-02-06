# Genesis ratelimit：令牌桶限流的核心原理与实现取舍

Genesis `ratelimit` 是治理层（L3）的限流组件，提供统一 `Limiter` 接口，支持两种运行模式：

- 单机模式（内存）
- 分布式模式（Redis + Lua）

本文聚焦核心机制：令牌桶模型、两种实现路径的差异、以及 HTTP/gRPC 接入时的执行语义。

---

## 0. 摘要

- 核心抽象是 `Limit{Rate, Burst}`：速率 + 桶容量。
- `Allow/AllowN` 是非阻塞尝试，`Wait` 仅单机模式支持。
- 单机模式基于 `x/time/rate`，按 `key+rate+burst` 维度缓存 limiter。
- 分布式模式用 Lua 脚本原子执行令牌计算，避免并发竞态。
- gRPC/Gin 默认“限流器异常时降级放行”，优先可用性。

---

## 1. 令牌桶模型：Rate 与 Burst 的工程语义

组件统一使用令牌桶：

- `Rate`：每秒补充多少令牌（长期吞吐上限）
- `Burst`：桶容量（短时突发上限）

如果请求一次要消费 `n` 个令牌：

- 桶中可用令牌 >= `n` => 允许
- 否则拒绝（`AllowN`）或等待（`Wait`，仅单机）

这让系统同时具备：

- 平稳速率控制（防止长期过载）
- 突发容忍（吸收短抖动）

---

## 2. 统一接口设计：行为先统一，再实现分流

`Limiter` 定义四个方法：

- `Allow(ctx, key, limit)`：尝试 1 个令牌
- `AllowN(ctx, key, limit, n)`：尝试 N 个令牌
- `Wait(ctx, key, limit)`：阻塞等待
- `Close()`：资源释放

重要边界：

- `key` 不能为空
- `Rate/Burst/n` 必须为正数
- 分布式模式 `Wait` 返回 `ErrNotSupported`

统一接口的价值在于：业务代码可按同一方式调用，再按部署拓扑选择驱动。

---

## 3. 单机模式原理：本地令牌桶 + 惰性实例缓存

### 3.1 状态组织

单机模式为每个“限流维度”维护一个本地 limiter：

- 缓存 key：`<bizKey>:<rate>:<burst>`
- 这意味着同一业务 key 在不同限流规则下会有独立桶

底层用 `sync.Map` 存储 limiter，并记录 `lastSeen` 访问时间。

### 3.2 判定路径

`AllowN` 流程：

1. 校验参数
2. 取/建对应 limiter
3. 调用 `limiter.AllowN(now, n)`
4. 更新 `lastSeen`

`Wait` 流程类似，但调用 `limiter.Wait(ctx)` 阻塞等待。

### 3.3 清理机制

后台协程按 `CleanupInterval` 扫描缓存：

- 若 `now - lastSeen > IdleTimeout` 则删除该 limiter

作用是防止高基数 key（如用户 ID、IP）导致内存长期增长。

---

## 4. 分布式模式原理：Redis Lua 原子限流

### 4.1 为什么用 Lua

分布式限流要解决的核心是“并发原子性”。  
如果把“读状态、算令牌、写回状态”拆成多条命令，会出现竞态。  
Lua 脚本把这一系列步骤放在 Redis 单线程执行，天然原子。

### 4.2 脚本中的时间戳令牌桶

当前实现保存的是“下一次可放行时间戳”而非显式 token 计数，关键变量：

- `interval_per_token = 1 / rate`
- `fill_time = capacity * interval_per_token`
- `last_refreshed`：上次记录的“时间基线”

判定逻辑：

1. `next_available_time = max(last_refreshed, now)`
2. `new_refreshed = next_available_time + requested * interval_per_token`
3. 若 `new_refreshed <= now + fill_time` => 允许，并写回 `new_refreshed`
4. 否则拒绝

脚本返回：

- 是否允许（1/0）
- 估算剩余令牌数

### 4.3 过期策略

允许请求时会给 key 设置过期时间：

- `EX = ceil(fill_time * 2)`

这样在长时间无请求后，限流状态会自动回收。

### 4.4 分布式模式不支持 Wait

`Wait` 在分布式场景要考虑：

- 多节点公平性
- 时钟与网络抖动
- 阻塞等待带来的资源占用

当前实现明确返回 `ErrNotSupported`，保持语义清晰。

---

## 5. 接入层语义：Gin 与 gRPC 的限流行为

### 5.1 Gin 中间件

执行顺序：

1. 计算 `key`（默认客户端 IP）
2. 计算 `limit`（默认空规则）
3. 规则无效时直接放行
4. 调用 `limiter.Allow`
5. 被拒绝返回 `429`

关键取舍：

- 限流器异常时默认放行（降级策略）
- 可选返回 `X-RateLimit-*` 头部

### 5.2 gRPC 拦截器

支持：

- Unary Server/Client
- Stream Server/Client

核心语义：

- 规则无效 => 放行
- 限流器报错 => 放行（降级）
- 明确限流 => 返回 `codes.ResourceExhausted`

流式拦截器采用 **Per-Stream 限流**（建流时检查一次），而不是 Per-Message。  
这是为了避免流中途突发限流导致协议层行为不可预期。

---

## 6. 选型建议：单机还是分布式

- 单机模式：
  - 低延迟、无外部依赖
  - 适合单实例或“每节点独立限流”
- 分布式模式：
  - 多实例共享同一限流状态
  - 适合网关级、全局配额级场景

常见组合：

- 在边缘层用分布式限流做全局保护
- 在服务实例内叠加单机限流做本地削峰

---

## 7. 设计取舍总结

`ratelimit` 的核心取舍是：

- 用统一接口屏蔽调用差异
- 用两种实现分别优化本地性能与分布式一致性
- 在接入层默认“失败放行”保证业务可用性

因此它不是单一算法封装，而是一套“算法 + 运行模型 + 接入语义”的治理能力组件。
