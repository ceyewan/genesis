# Genesis IDGen：多种 ID 生成能力的设计与取舍

Genesis `idgen` 是业务层（L2）的 ID 生成组件。它解决的不是某一个单点问题，而是把业务系统里最常见的几类 ID 需求放在同一个包下统一收口：本地趋势递增的整数主键、按业务键递增的序列号、实例级 WorkerID 分配，以及通用字符串唯一标识。

直接使用底层库当然也能做这些事：Snowflake 可以自己实现，Redis 的 `INCRBY` 可以自己封装，Etcd 租约也可以直接调用。但一旦把这些能力真正放进业务系统，就会碰到另一类问题：位布局如何定义，实例 ID 怎么分配，租约怎么保活，什么时候应该返回错误而不是吞掉，哪些能力应该统一暴露，哪些能力应该明确写出边界。这篇文章讨论的就是这些设计判断，而不是 API 罗列。

## 0 摘要

- `idgen` 提供四类能力：`Generator`、`UUID()`、`Sequencer` 和 `Allocator`。
- `Generator` 解决整数主键问题，`Sequencer` 解决同一业务键下递增问题，`Allocator` 解决 WorkerID 自动分配问题。
- `Generator` 当前支持两种显式模式：`single_dc` 使用 `41+10+12`，`multi_dc` 使用 `41+5+5+12`，并统一使用自定义 epoch `2024-01-01T00:00:00Z`。
- `Sequencer` 当前只支持 Redis，语义上更接近“带配置的 Redis 计数器”，而不是通用多后端抽象。
- `Allocator` 支持 Redis 和 Etcd；Etcd 天然有 lease，Redis 版本则需要额外处理所有权和幂等清理。
- `idgen` 不是“万能 ID 组件”，它更适合解决 Genesis 体系里的常见基础 ID 需求。

---

## 1 背景与问题

在业务系统里，ID 往往不是一个问题，而是几种完全不同的问题。

第一类是**全局主键**。这类 ID 需要足够短、趋势递增、适合数据库主键和索引，典型例子是订单 ID、支付流水 ID、用户主键。这种场景里，字符串 UUID 往往太长，数据库索引和排序体验也不理想，因此很多系统会转向 Snowflake 风格的整数 ID。

第二类是**业务局部序号**。这类 ID 并不要求全局唯一，而是要求同一个业务键下严格递增。最典型的是 IM 会话中的消息序号，或者某一类任务的执行序列号。这里用 Snowflake 并不能自然表达“同一会话内连续递增”这个需求，反而会把两个不相干的问题混在一起。

第三类是**实例级 WorkerID**。只要你想在多实例环境下使用 Snowflake，就必须先解决“每个实例拿到不同 WorkerID”这个问题。开发环境里手工配置还能忍，到了动态扩缩容、短生命周期实例和多机房场景，手工配置很快会变成运维负担。

第四类是**通用字符串唯一标识**。有些场景根本不想关心位布局、机房和 WorkerID，只需要一个足够通用、时间大致有序、可以跨系统传递的字符串 ID，这就是 UUID v7 的价值。

`idgen` 的存在意义，就是把这四类需求用一致的初始化模式和接口边界收拢起来。它不试图把所有 ID 统一成一种算法，而是承认这些问题本来就不同，然后分别给出合适的组件能力。

---

## 2 基础原理与前置知识

### 2.1 Snowflake 解决的是什么

Snowflake 的核心目标，是在不依赖中心服务的前提下，在本地进程里快速生成趋势递增且近似全局唯一的 64bit 整数 ID。它之所以适合数据库主键，不是因为“它是分布式算法”，而是因为它天然把时间顺序编码进了 ID，高位时间戳带来较好的插入局部性。

但 Snowflake 从来都不是“零配置”。一旦多实例部署，就必须给每个节点分配互不冲突的 worker bits。只要位布局、时钟回拨、实例编号管理三者中有一项处理不好，唯一性或有序性就会被破坏。

### 2.2 Sequencer 和 Snowflake 的本质区别

Snowflake 解决的是“全局主键”，Sequencer 解决的是“同一个业务键下递增”。这两个问题看起来都和“生成数字”有关，但语义完全不同。

如果业务真正想要的是 `conversation:123` 下的第 1、2、3、4 条消息，那么最自然的模型就是一个按 key 存储和递增的计数器。这里 Redis 的 `INCRBY` 比 Snowflake 更合适，因为它直接表达了“局部有序”的约束。

### 2.3 WorkerID 分配为什么要有租约

WorkerID 不是普通配置项，而是一个带所有权和生命周期的资源。静态配置虽然简单，但在弹性伸缩和短生命周期实例环境里不够稳。更合理的做法是让实例在启动时申请一个 ID，在退出或失联时自动释放，这就是 Allocator 的职责。

一旦引入租约，就要面对两个工程问题：谁拥有这个租约，如何判断租约是否还属于当前实例。Etcd 有原生 lease，Redis 没有，所以两者的实现复杂度天然不同。

---

## 3 设计目标

`idgen` 的设计目标可以概括为五条。

第一，**按问题拆能力，而不是强行统一算法**。Snowflake、序列号和 WorkerID 分配本来就不是一个问题，不应该用一个抽象硬包起来。

第二，**接口克制，初始化统一**。所有能力都遵循 `New(cfg, opts...)` 模式，让调用方只需要理解一套接入方式。

第三，**把正确性问题显式化**。比如 WorkerID 位宽、时钟回拨、租约失败，不应该通过静默降级掩盖。

第四，**尽量让主路径本地化**。`Generator` 是本地生成，`Sequencer` 和 `Allocator` 才依赖远端存储，各自承担不同的成本。

第五，**明确边界，不伪装成万能抽象**。Sequencer 现在只支持 Redis，就明确写出来；Allocator 的 Redis 版本需要所有权保护，也要在实现上真正处理。

---

## 4 核心接口与配置

`idgen` 当前暴露四类入口。

`Generator` 是 Snowflake 风格整数 ID 生成器：

```go
type Generator interface {
	Next() (int64, error)
	NextString() (string, error)
}
```

这里最关键的变化是：`Next` 和 `NextString` 都显式返回 error。这个设计很重要，因为时钟回拨、时间字段溢出这类问题并不是“返回一个哑值就行”的异常，而是调用方必须感知并处理的系统级错误。

`GeneratorConfig` 现在有三个关键字段：

| 字段 | 说明 |
| --- | --- |
| `Mode` | 位布局模式，`single_dc` 或 `multi_dc` |
| `WorkerID` | `single_dc` 范围 `0..1023`，`multi_dc` 范围 `0..31` |
| `DatacenterID` | `single_dc` 必须为 `0`，`multi_dc` 范围 `0..31` |

这里最重要的设计点，是**建议显式配置 `Mode`**。当前实现默认是 `multi_dc`，我们没有继续保留“`DatacenterID == 0` 时自动切到 10bit worker”这种隐式规则，因为它会把“多机房模式里的 0 号机房”和“单机房不占位”两个语义混在一起。显式 `Mode` 虽然多一个字段，但协议更清楚，解析也更稳定。

`Sequencer` 的接口是：

```go
type Sequencer interface {
	Next(ctx context.Context, key string) (int64, error)
	NextBatch(ctx context.Context, key string, count int) ([]int64, error)
	Set(ctx context.Context, key string, value int64) error
	SetIfNotExists(ctx context.Context, key string, value int64) (bool, error)
}
```

它的重点不在“生成一个数”，而在“以某个业务 key 为作用域，安全地推进一个序列”。

`Allocator` 的接口是：

```go
type Allocator interface {
	Allocate(ctx context.Context) (int64, error)
	KeepAlive(ctx context.Context) <-chan error
	Stop()
}
```

这里的关键点是 `KeepAlive` 返回的是错误通道，而不是“阻塞直到结束”的方法。它启动后台保活任务，调用方要做的是消费错误，而不是自己再包一层错误语义不清的 goroutine 协议。

---

## 5 核心概念与数据模型

### 5.1 Generator 的位布局

`Generator` 当前支持两套 64bit 位布局：

- `single_dc`：41bit 相对时间戳、10bit WorkerID、12bit 毫秒内序列号
- `multi_dc`：41bit 相对时间戳、5bit DatacenterID、5bit WorkerID、12bit 毫秒内序列号

时间字段不是直接存 Unix 毫秒，而是存“当前时间减去自定义 epoch”。`idgen` 当前使用的 epoch 是 `2024-01-01T00:00:00Z`。这样做的意义很直接：如果直接用 Unix 毫秒去占 41bit，时间窗口会被 1970 年到现在的历史时间消耗掉；引入自定义 epoch，则能把 41bit 的寿命留给真正需要的未来时间。

### 5.2 Sequencer 的 key 作用域

`Sequencer` 的核心数据模型其实很简单：`KeyPrefix + businessKey -> current sequence`。真正要小心的不是这个 map 结构本身，而是围绕它的语义扩展，例如步长、TTL、初始化和上限回绕。

一旦组件对外承诺了 `Set`、`SetIfNotExists`、`TTL` 和 `MaxValue`，调用方就会自然期待这些能力在不同写路径上的语义是一致的。这也是为什么实现里必须收紧这些行为，而不能让一部分写路径带 TTL、另一部分写路径不带。

### 5.3 Allocator 的所有权模型

Allocator 分配的不是一个普通整数，而是一个带**所有权**的 WorkerID。谁申请到，谁负责保活；谁失去所有权，就不能继续续租或删除这个租约。

Etcd 天然支持这种模型，因为 lease 本身就是一个服务端持有的资源句柄。Redis 没有 lease 语义，所以实现只能通过“key + instance value”的方式来模拟所有权。只按 key 操作而不验证 value，会把“当前实例持有的租约”和“某个 key 恰好存在”混为一谈，这是不够的。

---

## 6 关键实现思路

### 6.1 Generator：CAS + 显式错误

`Generator` 的热路径是本地内存操作。它用一个原子状态同时保存“上次毫秒时间”和“当前毫秒内 sequence”，通过 CAS 循环确保并发安全。

当系统时间正常前进时，逻辑很简单：如果还在同一毫秒，就递增 sequence；如果进入下一毫秒，就把 sequence 归零。难点在于时钟回拨。当前实现采用分级策略：

- 很小的回拨尝试复用上次时间
- 中等回拨短暂等待
- 超过阈值则直接返回错误

这里最关键的设计不是“怎么睡眠”，而是**错误必须被调用方看到**。如果生成失败时仍然返回一个看起来像正常值的结果，上层往往不会及时做出停机或告警判断。

### 6.2 Sequencer：Redis 原子脚本

`Sequencer` 的核心实现依赖 Redis Lua 脚本，把以下操作放在一个原子单元里：

- `INCRBY`
- 上限检查
- 必要时重置
- TTL 设置

这保证了高并发下的递增和批量分配不会被并发打断。这里的核心收益不是“Lua 很高级”，而是让“读当前值、算新值、写回、设置过期”这组动作在 Redis 侧一次完成。

### 6.3 Allocator：Redis 与 Etcd 的差异

Etcd 版本的 Allocator 相对直接。它用事务抢占一个尚未被占用的 key，再把 key 绑定到 lease 上。保活依赖 Etcd 的原生 `KeepAlive`，释放则通过 revoke lease 完成。

Redis 版本更复杂，因为 Redis 只提供 key/value 和过期时间。为了避免旧实例误续租或误删除新实例的 WorkerID，当前实现必须把“实例标识 value”也一起存进去，然后在 `KeepAlive` 和 `Stop` 里先校验 value，再决定是否续租或删除。这一层校验不是锦上添花，而是 Redis 版本正确性的必要条件。

---

## 7 工程取舍与设计权衡

### 7.1 为什么 Generator 不继续隐藏错误

一个常见诱惑是把生成失败吞掉，返回 `-1` 或空串，这样调用方接口更“简洁”。但这种简洁是假的。对 ID 组件来说，时钟回拨、时间字段溢出不是普通业务错误，而是会直接影响唯一性和有序性的系统错误。接口如果不暴露 error，只会把问题推迟到更难排查的位置。

### 7.2 为什么不把 Sequencer 做成通用多后端抽象

从抽象角度看，给 `Sequencer` 再补一个 Etcd 版本当然也可以。但当前代码和使用场景都更接近“带配置的 Redis 原子计数器”。在这种情况下，继续对外宣称它是一个成熟的 Redis/Etcd 双后端抽象，只会制造伪能力承诺。

所以当前更稳妥的选择是：实现只支持 Redis，就明确写 Redis；等未来确实有稳定的 Etcd 版本，再把能力边界打开。

### 7.3 为什么 Allocator 仍然保留 Redis 版本

从一致性和所有权模型看，Etcd 明显比 Redis 更自然。但保留 Redis 版本仍然有现实价值：很多项目已经有 Redis，没有 Etcd；而 WorkerID 分配的规模和一致性要求，很多时候还不到必须引入 Etcd 的程度。

因此这里的取舍不是“Redis 好还是 Etcd 好”，而是：既然要支持 Redis，就不能把 Redis 版本做成只在理想路径下能跑的实现，必须把所有权和幂等清理问题真正补齐。

### 7.4 为什么 UUID 仍然保留为独立入口

UUID v7 看起来和 Snowflake 都是在“生成一个唯一 ID”，但它们解决的是不同问题。保留 `UUID()` 这个零配置入口，恰恰体现了 `idgen` 的定位不是强迫所有场景都用一套位布局，而是承认“有些场景根本不需要 WorkerID 和整数主键”。

---

## 8 适用场景与实践建议

如果你的主键需要是整数、需要趋势递增、并且你愿意管理 WorkerID，那么优先用 `Generator`。这是 `idgen` 里最适合做数据库主键的一类能力。

如果你的业务真正需要的是某个 key 下的严格递增，例如同一会话里的消息序号，那么优先用 `Sequencer`。不要为了“全局统一”去拿 Snowflake 代替局部递增计数器。

如果 WorkerID 难以手工配置，或者实例数量会动态变化，那么把 `Allocator` 和 `Generator` 配合起来使用。生产场景里，如果你已经有 Etcd，优先考虑 Etcd 版本；如果只有 Redis，则至少要确保你真正消费了 `KeepAlive` 返回的错误通道。

如果你只需要一个通用字符串唯一标识，不想承担位布局和 WorkerID 的心智负担，那么直接用 `UUID()`。

容易踩的坑主要有三类。第一，忽略 `Generator` 的错误返回，把它当成“永不失败的本地函数”；第二，把 `Sequencer` 当成全局主键生成器使用；第三，启动 `KeepAlive` 后却不处理错误，导致租约丢失时业务层没有及时感知。

---

## 9 总结

`idgen` 的价值，不在于“提供了一种很厉害的 ID 算法”，而在于把业务系统里几类常见但性质不同的 ID 问题拆开处理，并用统一的接入模式收口。

`Generator` 负责趋势递增的整数主键，`Sequencer` 负责按键递增的业务序号，`Allocator` 负责 WorkerID 生命周期，`UUID()` 则提供了零配置的字符串唯一标识。它们彼此互补，而不是互相替代。

从 Genesis 的角度看，这也是一个很典型的组件设计原则：先把问题边界分清楚，再决定接口和实现；能显式暴露的风险不要藏起来；能明确写出的边界不要伪装成通用能力。
