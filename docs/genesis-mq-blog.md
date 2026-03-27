# Genesis mq：统一入口的持久化消息组件设计与实现

Genesis `mq` 是业务层（L2）的消息队列组件。它面向的核心问题是：微服务中不同团队或场景分别依赖 NATS JetStream 或 Redis Stream，连接管理、消息确认、重试、死信等横切逻辑被反复实现，且与具体中间件深度耦合。`mq` 的目标不是"把所有 MQ 都抽象成行为一致的接口"，而是提供一个**统一的接入方式**，让业务代码与底层驱动解耦，让 Retry、Logging、Recover 等横切逻辑可以跨驱动复用。

这篇文章解释：`mq` 的定位为什么是"统一入口"而不是"统一语义"，Ack/Nak 的边界如何取舍，消费者选项为什么会有跨驱动的语义差异，以及中间件层和生命周期各有哪些工程考量。

## 0 摘要

- `mq` 提供三个核心对象：`MQ`（发布订阅）、`Message`（消息）、`Subscription`（订阅句柄）。
- 当前后端两个：NATS JetStream 和 Redis Stream。两者均提供持久化和 At-least-once 投递，但 Nak 语义不同。
- `Ack()` 是稳定统一能力；`Nak()` 在 Redis 下返回 `ErrNotSupported`，不伪装成成功。
- 默认是手动确认（ManualAck）；AutoAck 模式统一处理跨驱动的 `ErrNotSupported`。
- 中间件层（Retry / Logging / Recover / DeadLetter）是横切逻辑的主承载点，独立于驱动实现。
- `Close()` 是幂等操作，关闭后 `Publish` 和 `Subscribe` 返回 `ErrClosed`。

---

## 1 背景

消息队列在微服务里是基础设施，但它比数据库或缓存更难抽象，原因在于：不同 MQ 系统的核心保证差异悬殊。

Redis Stream 是现有 Redis 部署的自然延伸。它有 Consumer Group 语义，可以做 At-least-once 交付，也有 `XAUTOCLAIM` 机制让崩溃的消费者遗留的 Pending 消息重新被认领。但它没有原生 Nak——消息被不 Ack 就只是留在 Pending，等待下次 XAUTOCLAIM 触发时重新分配。NATS JetStream 是一套完整的持久化流式系统，有明确的 Ack Wait 超时、Nak 触发立即重投、durable consumer 记录消费进度，流式能力更完整。

直接使用各自的 SDK 没有问题，但横切逻辑——连接注入、重试、日志、Panic 恢复、死信——就要被每套消费代码各自实现一遍。Genesis `mq` 的价值不在于"屏蔽差异"，而在于"共享基础设施层"：连接管理交给 Connector，横切逻辑交给中间件，`Handler` 只关心业务。

---

## 2 设计目标

三条核心目标支撑了后续所有取舍。

**接口克制，不做强统一**。`MQ` 接口只有 `Publish`、`Subscribe`、`Close`。`Message` 只有七个方法。不同驱动之间真正稳定统一的，就是这些。不稳定统一的，明确标出来，不伪装成稳定。

**显式依赖，不做自动注入**。连接器通过 `WithNATSConnector` / `WithRedisConnector` 显式传入，日志和指标通过 `WithLogger` / `WithMeter` 注入。没有全局状态，没有隐式默认连接。

**中间件在正确的位置**。重试、日志、Panic 恢复属于横切逻辑，不属于 Transport 实现，应该可以跨驱动复用。放在 `Middleware func(Handler) Handler` 这一层，后续增加驱动时不需要重新实现这些能力。

---

## 3 核心接口与配置

三个核心对象构成了完整的 API 面：

```go
type MQ interface {
    Publish(ctx context.Context, topic string, data []byte, opts ...PublishOption) error
    Subscribe(ctx context.Context, topic string, handler Handler, opts ...SubscribeOption) (Subscription, error)
    Close() error
}

type Handler func(msg Message) error

type Subscription interface {
    Unsubscribe() error
    Done() <-chan struct{}
}
```

`Handler` 只接收 `Message`，不接收独立的 `ctx`。这个设计让上下文通过 `msg.Context()` 传递，避免出现"Subscribe 时传入的 ctx"和"Handler 参数里的 ctx"两个上下文并存的语义歧义。

构造方式：

```go
q, err := mq.New(&mq.Config{
    Driver: mq.DriverNATSJetStream,
    JetStream: &mq.JetStreamConfig{
        AutoCreateStream: true,
        AckWait:          30 * time.Second,
    },
}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))
```

JetStream 和 Redis Stream 各有独立的配置块，只在对应驱动下生效。JetStream 的核心配置是 `AckWait`——超过这个时间没有 Ack，消息自动重投，建议设为业务 Handler 预期最大处理时间的 2 倍。Redis Stream 的核心配置是 `MaxLen` 和 `PendingIdle`——前者控制 Stream 裁剪策略，后者控制 Pending 消息多长时间后可被其他消费者认领。

---

## 4 核心概念与行为边界

### 4.1 "统一入口"而非"统一语义"

`mq` 的定位是：所有驱动的接入方式统一，但部分能力只在特定驱动上成立。

真正跨驱动稳定的能力：`Publish`、`Subscribe`、`Ack()`、`Headers`、`QueueGroup`（负载均衡消费组）、AutoAck / ManualAck 模式。这些能力在 JetStream 和 Redis Stream 上都有对等实现，语义足够接近。

不稳定统一的能力：`Nak()` 在 Redis 下返回 `ErrNotSupported`；`BatchSize` 对 JetStream 当前无效（push 模式）；`MaxInflight` 只对 JetStream 有意义。这类能力不能继续伪装成"所有驱动都支持"，否则调用方会依赖一个实际上不工作的功能。

这个区分不是文字游戏，而是影响调用方决策：如果你的错误处理依赖 Nak 立即重投，不要用 Redis Stream；如果你需要精确的 BatchSize 控制，不要使用 JetStream 当前的 push 模式。

### 4.2 Nak 的边界

`Nak()` 是 `mq` 接口设计中争议最多的方法。

JetStream 的 `Nak` 会触发消息立即重新投递给该 consumer。Redis Stream 没有这个机制——消息不 Ack 的唯一后果是它继续留在 Pending 列表，当 `PendingIdle` 超时后才会被 `XAUTOCLAIM` 捡起来重新处理。两者的恢复时间窗口差异显著：JetStream 是毫秒级，Redis 依赖 `PendingIdle` 配置，通常是秒到分钟级。

当前设计选择：Redis 的 `Nak()` 返回 `ErrNotSupported`，而不是返回 nil 制造"调用成功"的假象。这遵循"显式失败好过静默降级"的原则：调用方可以通过 `errors.Is(err, ErrNotSupported)` 知道这个操作没有执行，并选择是否做其他处理。AutoAck 模式下，`wrapHandler` 静默忽略 `ErrNotSupported`，因为 Redis 的恢复机制本来就不依赖显式 Nak。

### 4.3 QueueGroup 与 Durable 的语义差异

这两个选项在不同驱动下的映射关系是最容易产生误解的地方：

`WithQueueGroup(name)` 表达"多个消费者竞争消费同一 topic"，两个驱动都支持：

- JetStream：映射为 durable consumer 名称，多实例共享同一 durable，竞争消费，进度记录在 durable consumer 上。
- Redis：映射为 consumer group 名称，进度记录在 group 上，`WithQueueGroup` 是持久化订阅的主要手段。

`WithDurable(name)` 表达"消费者实例命名"，驱动语义不同：

- JetStream：映射为 durable consumer 名称（`WithQueueGroup` 为空时生效），是进度游标身份。
- Redis：映射为 consumer name，是同一 group 内消费者实例的标识，本身不承载消费进度。

实践上，需要多实例竞争消费时，优先用 `WithQueueGroup`——两个驱动都能正确工作，Redis 下这也是建立持久化进度的唯一方式。`WithDurable` 适合 JetStream 单消费者持久化场景，或 Redis 中为 consumer 命名以便监控区分。

---

## 5 关键实现思路

### 5.1 分层结构

```
MQ 对外接口（impl.go）
  └── Transport 内部接口
        ├── natsJetStreamTransport
        └── redisStreamTransport
Middleware（横切逻辑，独立于 Transport）
```

`Transport` 是 `mq` 内部接口，不暴露给调用方。驱动实现的变化对外完全透明。`impl.go` 专注于公共逻辑：closed state、指标记录、AutoAck 包装、`ErrNotSupported` 处理。

### 5.2 AutoAck 包装

`wrapHandler` 在 `impl.go` 中统一处理 AutoAck 逻辑：Handler 执行成功调用 `msg.Ack()`；Handler 执行失败调用 `msg.Nak()`，如果返回 `ErrNotSupported`（Redis 场景）则静默忽略。这个设计把跨驱动的 Ack/Nak 差异收归到一处处理，调用方在 AutoAck 模式下不需要关心底层驱动是否支持 Nak。

### 5.3 Redis Pending 恢复机制

Redis Consumer Group 消费循环中，`consumeWithGroup` 每隔 5 次循环执行一次 `XAUTOCLAIM`，认领空闲超过 `PendingIdle` 时间的 Pending 消息。这确保了即使消费者进程崩溃，Pending 消息也会在下次 claim 时被其他消费者接管。

XAUTOCLAIM 的游标持续推进，避免每次从 `0-0` 全量扫描 Pending 列表带来的性能问题。Consumer Group 在 `Subscribe` 时创建，失败立即向调用方返回错误，而不是在后台 goroutine 中静默失败。

### 5.4 JetStream 生命周期

JetStream 订阅使用 `consumer.Consume()`（push 模式）。返回的 `jetStreamSubscription` 在后台启动一个 goroutine，监听 `ctx.Done()`，触发时调用 `cons.Stop()` 并等待 `cons.Closed()`，最后关闭 `done` channel。这保证 `<-sub.Done()` 在 JetStream 内部完全停止消费后才返回，不会提前释放。

---

## 6 工程取舍与设计权衡

### 6.1 为什么不提供 NATS Core 驱动

NATS Core 是 fire-and-forget 语义：没有持久化，没有 Ack，消费者离线时消息丢失。这与 `mq` 组件提供持久化消息能力的定位不符。给调用方一个"看起来一样但可能悄悄丢消息"的驱动，风险大于价值。NATS Core 适合直接用 NATS SDK 处理实时通知场景，不适合进入 `mq` 的抽象层。

### 6.2 为什么 BatchSize 在 JetStream 下无效

JetStream 当前使用 `consumer.Consume()`——push 模式，服务端主动推送消息，不需要客户端主动 fetch。`BatchSize` 的语义是"单次 fetch 多少条"，在 push 模式下没有对应的调优点。要让 BatchSize 生效需要切换为 `consumer.Fetch(n)` 主动拉取模式，代价是消费循环需要由组件自己驱动，实现复杂度上升。当前选择 push 模式，BatchSize 在注释中明确标注对 JetStream 无效，不做静默假装生效。

### 6.3 为什么 Close 是幂等的

`mq.Close()` 设计为幂等：第二次调用返回 nil，不重复关闭底层 transport。原因是：调用方在 defer 链路中可能多次触发 Close（服务优雅退出、测试清理等），幂等性让调用方不需要做额外的"是否已关闭"检查。关闭后 `Publish` 和 `Subscribe` 返回 `ErrClosed`，调用方通过 `errors.Is` 检测。

### 6.4 为什么中间件不放在 Transport 里

中间件是"如何处理消息"的逻辑，与"从哪个后端收消息"无关。放在 Transport 层意味着每个驱动实现都要重复这些逻辑，或者引入复杂的继承关系。放在 `Middleware func(Handler) Handler` 这一层，后续增加 Kafka 驱动时只需新增 Transport 实现，中间件不需要变动，是横切能力复用最自然的位置。

---

## 7 适用场景与实践建议

`mq` 适合的典型场景：服务间异步事件通知（订单创建、用户注册后触发下游）、后台任务分发（邮件发送、报告生成）、多实例工作队列（多个消费者竞争处理同一 topic 的消息）。

两个驱动的选型原则：如果你的服务已经使用 Redis，且消息量在百万级以内，不想引入新的中间件，用 **Redis Stream**。如果你需要精确的 Nak 立即重投、复杂的流配置（保留策略、副本数、多 subject）或 NATS 生态的完整能力，用 **NATS JetStream**。

不适合 `mq` 的场景：消息量极大（亿级以上）且需要分区存储、Schema Registry 和精确回放的场景，这类需求更适合直接使用 Kafka SDK；实时广播且完全可以容忍消息丢失的场景（如在线人数推送），这类场景更适合直接使用 NATS Core 的 SDK，不需要通过 `mq` 的抽象层。

### 推荐实践

**默认 ManualAck，谨慎开启 AutoAck**。ManualAck 是默认值，不需要显式指定。AutoAck 意味着 Handler 返回 error 时自动触发 Nak——JetStream 下立即重投，Redis 下静默忽略等待 XAUTOCLAIM，两者行为窗口差距可能是几十秒。如果业务对幂等性有要求、或者需要对不同错误类型做不同处理，始终选手动确认。

**中间件推荐顺序：**

```go
handler = mq.Chain(
    mq.WithRecover(logger),                          // 最外层：捕获 panic，防止消费者崩溃
    mq.WithLogging(logger),                          // 记录每条消息的处理结果和耗时
    mq.WithRetry(mq.DefaultRetryConfig, logger),     // 内层：在应用层指数退避重试
)(businessHandler)
```

Recover 放在最外层：Handler panic 时，panic 会穿透 Retry 层被 Recover 捕获，返回 `ErrPanicRecovered`；此时 Retry 不会触发重试（因为 Recover 挡在外面）。如果想让 panic 也触发重试，把 Retry 放在 Recover 外层。

**JetStream `AutoCreateStream` 生产环境关闭**。开发阶段可以打开自动建 Stream，但生产环境的 Stream 应通过运维工具创建，明确配置保留策略（MaxMsgs、MaxAge、Storage 类型）、副本数等参数，避免自动创建使用默认值带来的意外行为。

**Redis 不要依赖 Nak 做立即重投**。业务失败时需要快速重试，应该用 `WithRetry` 中间件在应用层处理，不要期待 Redis 的 `Nak()` 能立即触发重投——它返回 `ErrNotSupported`，最终的重处理依赖 `PendingIdle` 超时。

### 常见误区

**误区一：认为 AutoAck 下两个驱动恢复时间相近**。JetStream 下 Nak 立即重投，Redis 下消息要等 `PendingIdle` 超时（默认 30 秒）才被 XAUTOCLAIM 重认领。同一业务逻辑在两个驱动下的故障恢复时间差异可能从毫秒到分钟。

**误区二：用 `WithDurable` 做 Redis 的持久化订阅**。Redis 持久化订阅的载体是 Consumer Group，用 `WithQueueGroup` 创建 group 后进度才会被持久化。`WithDurable` 在 Redis 下只是给消费者实例命名，不影响进度持久化。

**误区三：不关闭订阅直接关闭 MQ**。`mq.Close()` 不会主动取消已有订阅。在服务退出时，建议先调用所有 `sub.Unsubscribe()`，等待 `<-sub.Done()` 后再调用 `q.Close()`，确保消费循环干净退出，不丢失已接收但未处理的消息。

---

## 8 总结

Genesis `mq` 没有试图把 JetStream 和 Redis Stream 包装成行为完全一致的 MQ。它提供统一的接入方式，让业务代码与驱动解耦，同时让中间件可以跨驱动复用。`Ack` 是稳定统一的能力；`Nak` 在 Redis 下明确返回 `ErrNotSupported`，不做静默降级。默认手动确认，AutoAck 由上层统一处理跨驱动差异。中间件层与 Transport 层严格分离，`Close()` 幂等并守卫后续操作。

对于 Genesis 这种组件库来说，"让能力边界清晰可观察"比"让所有驱动看起来一致"更重要。
