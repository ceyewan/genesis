# Genesis cache：分布式、本地与多级缓存的接口边界设计

Genesis `cache` 是业务层（L2）的缓存组件。它面向的是微服务中最常见的几类缓存诉求：共享缓存、本地热点缓存，以及本地加远端组合的两级缓存。`cache` 不试图把所有后端能力都揉成一套最小公约数接口，而是把真正稳定的缓存共性沉淀为 `KV`，再把 Redis 特有但高频的结构化能力保留在 `Distributed` 上。

这篇文章不重复 README 的快速上手，而是解释 `cache` 为什么最终落成现在这组接口，为什么 `Local` 和 `Multi` 只保留 `KV`，为什么 `Distributed` 仍然保留 `Hash`、`Sorted Set`、`Batch` 与 `RawClient()`，以及这些边界背后的工程取舍。

## 0 摘要

- `cache` 提供三个构造入口：`NewDistributed`、`NewLocal`、`NewMulti`。
- `KV` 是稳定公共基座，`Local` 和 `Multi` 只暴露 `KV`。
- `Distributed` 当前明确面向 Redis，保留 `Hash`、`Sorted Set`、`MGet/MSet` 与 `RawClient()`。
- 接口刻意不提供 `List` 能力，因为它更像队列或日志容器语义，而不是缓存语义。
- `ttl <= 0` 统一表示使用组件配置中的 `DefaultTTL`，避免自定义特殊规则。
- `Multi` 是缓存策略层，而不是新的存储引擎；它的职责是本地命中、远端回源与回填。

---

## 1 背景

缓存组件很容易失控。最常见的失控方式有两种。

第一种是抽象过度。为了让业务层"永远只看见一个接口"，把本地缓存、Redis、两级缓存全部塞进一个庞大的总接口里，最后得到的是一套语义模糊、边界不清、行为并不稳定的 API。调用方虽然表面上只依赖一个接口，实际却不得不了解不同实现背后的差异。

第二种是抽象不足。业务代码直接面向底层 Redis 客户端开发，确实保留了所有原生能力，但也把连接管理、序列化、错误模型、前缀隔离、测试入口全部散落到各个服务里。短期内很直接，长期却很难维护。

Genesis `cache` 的定位是在这两种极端之间取得平衡。它不做"伪统一"，只抽象真正稳定的缓存共性；同时又保留 Redis 场景下确实高频、且业务上有明确价值的扩展能力。

---

## 2 设计目标

`cache` 的设计目标可以概括为四条。

第一，**公共语义尽量小而稳定**。`KV` 只包含 `Set/Get/Delete/Has/Expire/Close` 六个能力，这些能力对于本地缓存、Redis 缓存和两级缓存都成立。

第二，**本地缓存和多级缓存不伪装成 Redis**。`Local` 和 `Multi` 只做 `KV`，不再假装支持 `Hash`、`Sorted Set` 或其他结构化能力。否则调用方得到的不是统一，而是"接口统一，行为分裂"。

第三，**远端缓存保留 Redis 导向的高频能力**。Genesis 当前明确使用 Redis 作为分布式缓存后端，因此 `Distributed` 保留 `Hash`、`Sorted Set`、`Batch` 和 `RawClient()`，而不是为了"也许以后换后端"把接口设计成最小公约数。

第四，**策略与存储分离**。`Multi` 的职责是组合 `Local` 和 `Distributed`，实现读回源、回填与写通语义；它不是新的存储引擎，也不应该扩展出 Redis 风格的数据结构接口。

---

## 3 核心接口与配置

`cache` 最终对外暴露三类接口：`Distributed`、`Local`、`Multi`。它们共享同一个公共基座 `KV`。

```go
type KV interface {
    Set(ctx context.Context, key string, value any, ttl time.Duration) error
    Get(ctx context.Context, key string, dest any) error
    Delete(ctx context.Context, key string) error
    Has(ctx context.Context, key string) (bool, error)
    Expire(ctx context.Context, key string, ttl time.Duration) (bool, error)
    Close() error
}
```

这里最重要的不是方法名，而是语义约定。`Get` 未命中时返回 `ErrMiss`，`Has` 通过布尔值表达存在性，`Expire` 用 `(bool, error)` 表达"是否存在"和"是否执行成功"这两个维度。相比把"未命中"折叠进错误文本，这种设计更稳定，也更容易在业务代码中判断。

`Distributed` 在 `KV` 之上保留 Redis 场景真正高频的扩展能力：

```go
type Distributed interface {
    KV
    HSet(ctx context.Context, key, field string, value any) error
    HGet(ctx context.Context, key, field string, dest any) error
    HGetAll(ctx context.Context, key string, destMap any) error
    HDel(ctx context.Context, key string, fields ...string) error
    HIncrBy(ctx context.Context, key, field string, increment int64) (int64, error)
    ZAdd(ctx context.Context, key string, score float64, member any) error
    ZRem(ctx context.Context, key string, members ...any) error
    ZScore(ctx context.Context, key string, member any) (float64, error)
    ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error
    ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error
    ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error
    MGet(ctx context.Context, keys []string, destSlice any) error
    MSet(ctx context.Context, items map[string]any, ttl time.Duration) error
    RawClient() any
}
```

这里有两个刻意的边界。第一，保留 `Hash`、`Sorted Set` 和 `Batch`，因为它们仍然属于"缓存数据建模"的常见能力。第二，接口不提供 `List`，因为 `List` 更接近队列、日志窗口、消费缓冲等语义，把它放在 `cache` 中只会让组件边界越来越模糊。

构造函数也做了显式收敛：

```go
func NewDistributed(cfg *DistributedConfig, opts ...Option) (Distributed, error)
func NewLocal(cfg *LocalConfig, opts ...Option) (Local, error)
func NewMulti(local Local, remote Distributed, cfg *MultiConfig) (Multi, error)
```

这组入口背后的原则是：连接器、日志、指标是可选依赖，可以通过 `Option` 注入；但 `Multi` 的 `local` 与 `remote` 是核心依赖，必须显式传参，不再藏在 `Option` 里。

---

## 4 核心概念与行为边界

### 4.1 `Local` 是进程内缓存，不是迷你 Redis

`Local` 的正式契约很简单：只支持 `KV`，使用值语义，不暴露底层 otter，也不提供 `Hash`、`Sorted Set`、`Batch` 这类 Redis 风格能力。

这背后的原因不是"实现不了"，而是"没有必要"。一旦本地缓存开始暴露复杂数据结构，调用方就会期待两件事：一是行为和 Redis 一致，二是这些能力也能自然进入 `Multi`。这两件事都会把 `cache` 从"缓存组件"拖向"统一数据结构 SDK"，而那不是它的目标。

### 4.2 `Multi` 是策略层，不是能力聚合层

`Multi` 的读路径是 `local -> distributed -> backfill local`，写路径是 `write distributed -> write local`，删路径是 `delete distributed + delete local`。这让它成为一个很典型的两级缓存策略组件。

但它不应该承载远端的所有复杂能力。`Hash`、`Sorted Set` 等接口进入 `Multi` 后，调用方会自然地期待"本地是否也要缓存 Hash 字段""回填是否要做局部字段同步""冲突时以谁为准"等更复杂的问题。这些问题并不是简单的接口扩展，而是在引入新的缓存一致性模型。Genesis 当前不打算在 `cache` 中解决这类问题，因此 `Multi` 只保留 `KV` 是有意设计。

### 4.3 `RawClient()` 是逃生口，不是常规路径

Redis 的高级能力不可能都做成公共抽象，比如 Pipeline、Lua 脚本、事务命令、特殊管理命令等。如果完全不留逃生口，业务代码最终只会绕过 `cache`，直接拿连接器去做自己的实现。

因此 `Distributed` 保留 `RawClient()`，但这个方法的定位必须清楚：它是 escape hatch，用于高级场景，不保证跨后端兼容，也不应该成为常规业务读写路径。

---

## 5 关键实现思路

### 5.1 统一序列化边界

无论是 `Distributed` 还是 `Local`，写入时都先做统一序列化，读取时再反序列化到目标对象。这样带来的直接好处是：调用方操作的是结构化对象，而不是被迫手写字符串编解码逻辑；与此同时，值语义也更容易保证，尤其是在本地缓存场景下，调用方修改原对象或读取结果，不会直接污染缓存内部状态。

序列化器通过配置可插拔，默认使用 JSON。在高吞吐场景下可以切换为 `msgpack`——后者序列化速度约快 2 倍，数据体积也更小，代价是可读性下降。两者在接口层是完全透明的，调用方不需要改任何代码，只需修改 `Serializer` 配置项。

### 5.2 本地缓存的原子 TTL 设置

本地缓存底层使用 otter v2。otter 的 `Set` 和修改 TTL 的 `SetExpiresAfter` 是两个独立调用，直接组合会在并发写同一 key 时产生竞态——先写入的数据可能被后一次写入的 TTL 覆盖。

`cache` 的解法是引入一个内部包装类型 `localEntry{data []byte, ttl time.Duration}`，利用 otter 的 `ExpiryWritingFunc` 在每次写入时从 entry 自身读取 TTL，将数据和 TTL 合并进单次 `Set` 调用：

```go
cache.Set(key, localEntry{data: serializedData, ttl: resolvedTTL})
```

这样无论并发多少，每次写入都是原子的，TTL 永远和数据绑定在一起。

### 5.3 Multi 的 FailOpen 机制

本地缓存虽然是进程内组件，理论上也可能返回错误（如底层内存异常）。`Multi` 通过 `FailOpenOnLocalError`（默认 `true`）控制遇到本地错误时的行为：

- `true`：忽略本地错误，继续访问远端——这是推荐的生产配置，本地层是加速层，不是阻断层。
- `false`：本地错误时直接返回——适合对数据一致性要求极高、需要明确感知本地异常的场景。

这个策略同时应用于读、写、删、过期四条路径，保持语义一致。

### 5.4 多级缓存的一致性以远端为准

`Multi` 的本地层默认是优化层，而不是权威数据源。写入、删除和过期操作都优先以远端结果为准，再去同步本地状态。比如远端 `Expire` 失败或发现 key 已不存在时，本地层也必须同步失效，避免继续返回陈旧副本。这个规则让 `Multi` 的一致性模型保持清晰：本地缓存负责提速，不负责定义新的数据生命周期。

---

## 6 工程取舍与设计权衡

### 6.1 为什么保留 `Hash`、`Sorted Set`，却不提供 `List`

`Hash` 和 `Sorted Set` 仍然属于缓存建模里很常见的能力。前者适合对象字段缓存和计数器，后者适合排行榜、时间窗口和有序索引。它们保留在 `Distributed` 中，能显著降低业务侧重复写 Redis 访问代码的成本。

`List` 的情况不同。虽然 Redis `List` 也能"存东西"，但在实际系统中它更多被拿来做消费队列、消息缓冲、最近记录窗口等场景。这些场景和"缓存命中、失效、回源"的核心模型已经不在一条线上。把 `List` 放在 `cache` 包里，会让这个组件继续向"Redis 结构封装库"扩张，因此接口刻意不提供这个能力。

### 6.2 为什么不保留一个总入口 `New`

旧式的统一入口通常会让调用方在一个 `Config` 里塞入所有驱动相关字段，再通过 `driver` 做内部派发。这样做的表面好处是"入口统一"，但缺点也很明显：配置体会不断变大，不同模式之间会共享许多根本不相关的字段，调用方也要始终知道当前自己到底在构造哪一种缓存。

Genesis 最终采用了三个显式构造函数。这样虽然入口多了两个，但依赖关系、配置边界和资源所有权都更清楚，长期维护成本更低。

### 6.3 为什么 `Distributed` 接口面向 Redis 而不是通用抽象

如果后端种类很多，把 `HashStore`、`SortedSetStore`、`BatchStore` 都拆成独立能力接口是合理的。但 Genesis 当前在分布式缓存上明确使用 Redis，过早为"也许以后会换后端"做抽象，只会增加接口层级和调用复杂度。因此当前方案是：`Distributed` 直接承载 Redis 导向能力，未来如果后端模型真的变化，再考虑细分。

---

## 7 适用场景与实践建议

如果你的服务需要共享缓存、结构化缓存对象、排行榜或批量拉取热点 key，`Distributed` 是推荐入口。如果你的热点数据只在单进程内复用，或者你需要一个短 TTL 的本地加速层，`Local` 更合适。如果你的目标是降低常见读路径延迟，同时又希望由 Redis 保持权威状态，那就使用 `Multi`。

不适合 `cache` 的场景也应该说清楚。如果你的核心诉求是消息投递、队列消费、日志窗口或流式处理，这些能力更适合进入 `mq` 或其他专门组件。如果你的业务严重依赖复杂 Redis 原生命令组合，那么 `RawClient()` 是可接受的出口，但这也意味着你的抽象需求已经超出了 `cache` 的主路径。

### 推荐配置

生产环境中，`DefaultTTL` 应根据业务数据的实际生命周期设置，而不是依赖默认值。`Distributed` 的默认值是 24 小时，`Local` 的默认值是 1 小时——这是兜底保障，不应该成为业务的数据保留策略，每次写入都应该传入显式 TTL。

`KeyPrefix` 建议按服务或模块设置，例如 `"order:"` 或 `"user:v2:"`，避免多个服务共用同一 Redis 实例时的 key 冲突。当业务模型有重大变更时，通过修改 prefix 版本号可以实现批量失效，比逐 key 删除更简洁。

序列化器的选择取决于场景：JSON 可读性好，调试和运维友好；msgpack 性能更优，适合高吞吐、value 体积较大的场景。两者在接口层完全透明，可以按需切换。

对于 `Multi`，`BackfillTTL` 应显著短于 `Distributed` 的 TTL，以避免本地副本在远端数据已变更后仍长期存在。一般建议 `BackfillTTL` 不超过几分钟。

### 常见误区

**误区一：忘记关闭 `Local`。** `Local` 内部启动了 goroutine 用于过期扫描，如果不调用 `Close()`，这些 goroutine 会一直存活。服务退出时应确保 `defer local.Close()` 被执行。

**误区二：把 `Multi.Close()` 当成关闭全部资源。** `Multi` 不拥有 `local` 和 `remote` 实例，`Close()` 是 no-op。调用方需要分别关闭 `local` 和 `redisConn`。

**误区三：在 `Multi` 上调用 `RawClient()`。** `Multi` 实现的是 `KV` 接口，没有 `RawClient()`。如果业务需要高级 Redis 操作，应该直接持有 `Distributed` 实例，而不是通过 `Multi` 传递。

**误区四：依赖 `DefaultTTL` 作为长期持久化策略。** `DefaultTTL` 只是兜底，不是承诺。Redis 在内存压力下会触发 LRU 淘汰，无论 TTL 是否到期。业务上需要持久化的数据不应该只存缓存。

---

## 8 总结

Genesis `cache` 最终没有走"所有缓存能力都统一成一个大接口"的路线，而是选择了更克制的边界：`KV` 作为稳定公共基座，`Local` 与 `Multi` 只做 `KV`，`Distributed` 明确面向 Redis 并保留高频结构化能力，`RawClient()` 作为高级场景的逃生口，`List` 则刻意不纳入接口。

这样的设计不追求抽象上的整齐，而是追求工程上的稳定。对于 Genesis 这种组件库来说，这比"把所有东西都做成看起来统一"更重要。
