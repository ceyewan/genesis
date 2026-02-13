# Genesis mq：多后端统一消息队列抽象与实践

Genesis `mq` 是业务层（L2）的消息队列抽象组件，旨在通过一套统一的 API 覆盖 NATS Core、NATS JetStream 和 Redis Stream 等多种消息后端。它在保持接口简洁的同时，尊重不同后端的语义差异，为微服务架构提供灵活、可插拔的异步通信能力。

---

## 0. 摘要

- **统一抽象**：提供标准化的 `Publish` / `Subscribe` 接口，屏蔽底层连接与协议细节
- **多端支持**：内置 `nats_core`（低延迟）、`nats_jetstream`（持久化）、`redis_stream`（轻量级）三种驱动
- **概念映射**：将不同后端的"消费者组"、"持久化"、"确认机制"映射到统一的 Option 选项
- **依赖注入**：遵循 Genesis 规范，通过 `WithNATSConnector` / `WithRedisConnector` 显式注入底层连接
- **可观测性**：深度集成 `clog` 和 `metrics`，自动记录消息收发日志与延迟指标

---

## 1. 背景：微服务中的消息中间件选型困境

在微服务开发中，消息队列（MQ）是解耦、削峰填谷的关键组件。然而，面对不同的业务场景，开发者常常陷入选型困境：

- **实时通知**：需要 NATS Core 这样的毫秒级低延迟组件，但不需要持久化。
- **关键任务**：订单支付等场景需要 NATS JetStream 或 Kafka 这样可靠的持久化队列。
- **轻量级应用**：小规模系统希望复用现有的 Redis 设施（Redis Stream），不想引入繁重的 MQ 运维成本。

如果在业务代码中直接耦合某个具体的 MQ SDK，一旦需求变更或架构演进（例如从 Redis Stream 迁移到 JetStream），代码改造的成本将极其高昂。Genesis `mq` 组件正是为了解决这一痛点，通过定义一套通用的消息操作接口，让业务代码与具体实现解耦。

---

## 2. 核心设计：统一接口与配置驱动

### 2.1 核心接口

`mq` 组件的接口设计极度克制，仅暴露最核心的发布订阅能力：

```go
type MQ interface {
    // Publish 发布消息
    Publish(ctx context.Context, topic string, data []byte, opts ...PublishOption) error

    // Subscribe 订阅消息
    // handler: 消息处理函数，func(msg Message) error
    Subscribe(ctx context.Context, topic string, handler Handler, opts ...SubscribeOption) (Subscription, error)

    // Close 关闭资源
    Close() error
}
```

### 2.2 驱动选择

通过 `Config.Driver` 字段灵活切换底层实现，无需修改业务逻辑代码：

- `nats_core`：适用于 ephemeral（临时）消息，如 UI 实时更新。
- `nats_jetstream`：适用于需要持久化、At-Least-Once 投递的场景。
- `redis_stream`：适用于中等规模、已使用 Redis 的场景。

---

## 3. 后端适配与语义差异

虽然 API 统一，但不同后端的底层语义（尤其是持久化和消费模式）存在客观差异。Genesis `mq` 通过 Option 模式尽可能抹平使用方式，但开发者仍需理解其行为边界。

| 特性         | NATS Core           | NATS JetStream  | Redis Stream       | 语义说明                        |
| :----------- | :------------------ | :-------------- | :----------------- | :------------------------------ |
| **持久化**   | 否（内存转发）      | 是（磁盘/文件） | 是（AOF/RDB）      | 消息是否落盘，重启是否丢失      |
| **消费者组** | Queue Group         | Consumer        | Consumer Group     | `WithQueueGroup` 选项的映射     |
| **确认机制** | 无（Fire & Forget） | 显式 Ack/Nak    | XACK               | `WithAutoAck` / `WithManualAck` |
| **批处理**   | 弱                  | 支持            | `XREADGROUP COUNT` | `WithBatchSize` 提升吞吐        |
| **消息回溯** | 不支持              | 支持            | 支持               | 是否能消费历史消息              |

---

## 4. 关键特性实现

### 4.1 负载均衡与消费者组

在微服务中，"多个实例共同消费一个 Topic，每条消息只被一个实例处理"是刚需。`mq` 组件通过 `WithQueueGroup(name)` 选项统一实现了这一能力：

- **NATS Core**：映射为 `QueueSubscribe`。组内竞争消费，组间广播。
- **JetStream**：映射为 Shared Durable Consumer。
- **Redis Stream**：映射为 Consumer Group。自动创建组，并通过 `XREADGROUP` 抢占消息。

### 4.2 批处理与吞吐优化

为了提升高并发场景下的吞吐量，`mq` 提供了 `WithBatchSize(n)` 选项：

- **Redis Stream**：直接对应 `XREADGROUP COUNT n`，显著减少网络往返（RTT）。
- **JetStream**：内部实现会利用 Fetch 批量拉取能力（视具体驱动实现版本而定）。

### 4.3 消息确认与可靠性

对于金融级业务，消息绝不能丢。`mq` 支持手动确认模式：

```go
// 订阅时开启手动确认
mq.Subscribe(ctx, "orders", func(msg mq.Message) error {
    // 处理业务...
    if err := process(msg); err != nil {
        return err // 返回 error 会触发 Nak 或重试
    }
    return msg.Ack() // 显式确认
}, mq.WithManualAck())
```

- **Redis Stream**：`Ack` 调用 `XACK`。
- **JetStream**：`Ack` 调用 JetStream 的 Ack 确认。
- **NATS Core**：忽略 Ack 操作（因其本身不支持）。

---

## 5. 实战落地

### 5.1 初始化（以 Redis Stream 为例）

```go
// 1. 初始化 Redis 连接器 (L1)
rdb, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer rdb.Close()

// 2. 初始化 MQ (L2)，注入 Redis 连接
q, _ := mq.New(&mq.Config{
    Driver: mq.DriverRedisStream,
}, mq.WithRedisConnector(rdb), mq.WithLogger(logger))
```

### 5.2 生产者：发布订单消息

```go
orderData := []byte(`{"id": "1001", "amount": 99.0}`)
err := q.Publish(ctx, "orders.created", orderData)
if err != nil {
    logger.Error("failed to publish order", clog.Error(err))
}
```

### 5.3 消费者：组消费模式

```go
// 启动 3 个并发协程作为消费者组 "order-processor" 的成员
for i := 0; i < 3; i++ {
    go func(id int) {
        _, err := q.Subscribe(ctx, "orders.created", func(msg mq.Message) error {
            logger.Info("processing order",
                clog.String("worker", fmt.Sprintf("%d", id)),
                clog.String("data", string(msg.Data())))
            return nil // 自动 Ack
        },
        mq.WithQueueGroup("order-processor"), // 指定消费者组
        mq.WithBatchSize(10))                 // 批量拉取优化

        if err != nil {
            logger.Fatal("subscribe failed", clog.Error(err))
        }
    }(i)
}
```

---

## 6. 最佳实践与常见坑

### 6.1 死信队列（DLQ）处理

当前版本的 `mq` 组件预留了 `WithDeadLetter` 接口，但建议在**业务层**实现死信逻辑，以保证最大可控性：

1.  使用 `WithRetry` 中间件进行有限次重试。
2.  重试耗尽后，业务代码捕获错误，手动将消息 Publish 到 `xxx.dlq` 主题。
3.  对原消息进行 Ack，避免阻塞队列。

### 6.2 选型建议

- **优先 Redis Stream**：如果你不想引入新的中间件，且消息积压量在百万级以内，Redis Stream 是性价比之选。
- **优先 JetStream**：如果你追求极致性能且需要复杂的持久化策略（如文件存储、内存存储混合），JetStream 是 NATS 生态的最佳实践。
- **慎用 NATS Core**：仅在完全可以容忍消息丢失（如在线人数统计、即时弹幕）的场景使用。

---

## 7. 总结

Genesis `mq` 组件通过精巧的接口抽象，在不牺牲底层特性的前提下，实现了多后端消息队列的统一管理。它让开发者可以从具体的中间件运维中解放出来，专注于业务逻辑的实现，同时也为未来的架构演进预留了充足的灵活性。
