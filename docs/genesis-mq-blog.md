# Genesis mq：NATS、Redis Stream 与 Kafka 的消息语义与工程实践

Genesis `mq` 是业务层（L2）的消息队列抽象组件，目标是用一套统一 API 覆盖常见发布/订阅场景，同时保留不同后端的关键差异。本文重点回答四个工程问题：

- 多个消费者如何负载均衡？
- 消费者组在不同后端如何实现？
- 批处理如何提升吞吐？
- 死信队列如何落地？

---

## 0. 摘要

- 当前 `mq` 已实现驱动：`nats_core`、`nats_jetstream`、`redis_stream`。
- `kafka` 在代码中是预留驱动常量，当前版本未实现 Transport。
- 统一入口是 `WithQueueGroup`，但底层语义不同：
  - NATS Core：Queue Subscribe 竞争消费
  - NATS JetStream：共享 Durable Consumer 竞争消费
  - Redis Stream：Consumer Group（`XREADGROUP`）竞争消费
- 批处理能力当前以 Redis Stream 最完整（`XREADGROUP COUNT`），JetStream 具备能力标记但当前实现未使用 `BatchSize` 参数。
- 死信配置 `WithDeadLetter` 已预留，但当前驱动实现均未生效，需要在业务层或基础设施层补齐。

---

## 1. 组件定位：统一 API，不抹平语义差异

`mq` 的核心接口只有三件事：

- `Publish(ctx, topic, data, opts...)`
- `Subscribe(ctx, topic, handler, opts...)`
- `Close()`

常用订阅选项：

- `WithQueueGroup(name)`：负载均衡入口
- `WithDurable(name)`：持久化消费者标识（JetStream/Redis）
- `WithManualAck()` / `WithAutoAck()`：确认策略
- `WithBatchSize(n)`：批量读取大小（当前 Redis 生效最明显）
- `WithMaxInflight(n)`：在途消息上限（JetStream）
- `WithDeadLetter(maxRetries, topic)`：死信预留配置（当前未生效）

这套抽象刻意“统一调用方式”，但不会假装后端完全一致。

---

## 2. 三种后端 + Kafka 的能力边界

| 能力 | NATS Core | NATS JetStream | Redis Stream | Kafka（当前状态） |
| :--- | :--- | :--- | :--- | :--- |
| 持久化 | 否 | 是 | 是 | 预期是 |
| Ack | 无持久化 Ack 语义 | 显式 Ack/Nak | `XACK` | 依赖 offset commit（未接入） |
| 队列组/消费者组 | Queue Group | Durable Consumer | Consumer Group | Consumer Group（未接入） |
| 批处理 | 弱 | 有能力但当前实现未显式接线 `BatchSize` | `XREADGROUP COUNT` | 天然支持（未接入） |
| 死信队列 | 否 | 需额外配置 | 需额外实现 | 通常用 DLT（未接入） |

结论：如果你现在就需要“成熟的持久化 + 批量消费 + 崩溃恢复”，优先用 `redis_stream` 或 `nats_jetstream`；Kafka 目前只能作为对比和未来演进方向。

---

## 3. 多消费者负载均衡：同一个入口，不同底层机制

### 3.1 NATS Core：Queue Subscribe

- 订阅时设置 `WithQueueGroup("workers")`，底层走 `QueueSubscribe(topic, group, cb)`。
- 同组消费者竞争同一主题消息，每条消息只投递给组内一个实例。
- 不同组之间相互独立，相当于“组间广播，组内负载均衡”。
- 无持久化，消费者断线期间消息会丢失。

适用场景：低延迟、可容忍丢消息的实时任务。

### 3.2 NATS JetStream：共享 Durable Consumer

- `WithQueueGroup("workers")` 会映射为同名 Durable Consumer。
- 多个实例绑定同一 Durable 时，JetStream 在实例间分发消息。
- `WithMaxInflight(n)` 映射 `MaxAckPending`，用于背压。
- 自动确认模式下，`handler` 成功会 `Ack`，失败会 `Nak`（后端支持时）。

适用场景：既要负载均衡，又要消息持久化和重投能力。

### 3.3 Redis Stream：Consumer Group + Pending 恢复

- `WithQueueGroup("workers")` 对应 Redis Consumer Group。
- 每个消费者实例有独立 consumer name（`WithDurable` 可指定）。
- 读取新消息用 `XREADGROUP ... >`，确认用 `XACK`。
- 未确认消息进入 Pending 列表；实现中定期 `XAUTOCLAIM` 认领超时消息，避免消息“卡死”。

适用场景：需要持久化、组消费、崩溃恢复且基础设施已广泛使用 Redis。

### 3.4 Kafka（对比说明）

Kafka 的负载均衡基于“分区 + 消费者组”：组内实例各自分配分区并消费分区内有序消息。  
Genesis `mq` 当前尚未实现 Kafka Transport，但接口上已为这类语义预留了 `WithKey`、`DriverKafka`、`CapabilitiesKafka`。

---

## 4. 消费者组如何实现

### 4.1 统一心智模型

可以把 `WithQueueGroup("g1")` 理解成“组标识”，再映射到各后端：

- NATS Core：Queue Group 名
- JetStream：Durable Consumer 名
- Redis Stream：Consumer Group 名
- Kafka：Consumer Group ID（未来）

### 4.2 组内与组间语义

- 组内：竞争消费（负载均衡）
- 组间：相互独立（广播）

这也是“一个 topic 供多个业务域订阅”的基础能力。例如：`orders.created` 可同时被风控组、积分组消费，各组内再水平扩容。

---

## 5. 吞吐优化：批处理与背压

### 5.1 Redis Stream 批处理（当前最可控）

`WithBatchSize(n)` 映射到 `XREADGROUP COUNT n` / `XREAD COUNT n`。  
典型收益：

- 降低网络往返次数
- 减少 Redis 命令开销
- 提高单消费者吞吐

建议起步：

- `BatchSize` 从 `50` 或 `100` 开始压测
- 结合业务处理耗时，观察端到端延迟
- 若积压增大，优先扩消费者实例，再调批量

### 5.2 JetStream 背压控制

- `WithMaxInflight(n)` 映射 `MaxAckPending`，限制未确认消息数。
- 可防止消费者处理能力不足时无限堆积在本地处理路径。

### 5.3 重要现状说明

当前代码中 `WithBatchSize` 在 JetStream 实现里尚未显式参与拉取参数设置。  
如果你要“强可控批拉取”，当前版本建议优先 Redis Stream；JetStream 可先用 `MaxAckPending` 做背压，再通过并发度调优。

---

## 6. 死信队列（DLQ）：当前状态与落地策略

### 6.1 当前状态

- `WithDeadLetter(maxRetries, topic)` 已有 API。
- 但三种已实现驱动当前均未真正执行 DLQ 路由逻辑。
- 即：调用该选项不会报错，但不会自动把失败消息投递到死信队列。

### 6.2 当前可落地方案（建议）

方案 A：业务层重试 + 显式旁路 DLQ（推荐，立即可用）

1. 用 `WithRetry` 中间件做有限次重试。
2. 重试仍失败时，发布到 `${topic}.dlq`（或约定的错误主题）。
3. 原消息 `Ack`，避免无限重投风暴。

方案 B：JetStream 基础设施层实现 DLQ

1. 配置消费者最大投递次数（如 `MaxDeliver`）。
2. 超限后转发到专门 Stream/Subject。
3. 运维订阅 DLQ 做告警和补偿处理。

方案 C：Redis Stream Pending 扫描转 DLQ

1. 周期扫描 `XPENDING` / 认领超时消息。
2. 维护重试次数（可放消息头或业务字段）。
3. 超限后 `XADD` 到 `*.dlq`，并对原消息 `XACK`。

### 6.3 Kafka 对应模式（未来接入）

Kafka 通常把失败消息写到 DLT（Dead Letter Topic），并保留原 topic、partition、offset、error reason 作为排障元数据。

---

## 7. 选型建议：什么时候用哪种后端

- 优先 NATS Core：追求极低延迟，可容忍丢消息。
- 优先 NATS JetStream：需要持久化 + Ack/Nak + NATS 生态。
- 优先 Redis Stream：需要可靠组消费、批量拉取、并且团队 Redis 运维成熟。
- 规划 Kafka：需要超大吞吐、分区有序、生态化流处理（当前 Genesis `mq` 需后续实现）。

---

## 8. 一份可执行的工程基线

以“可靠消费”目标为例，可采用：

1. 驱动：`redis_stream` 或 `nats_jetstream`
2. 订阅：`WithQueueGroup("xxx-workers")`
3. 确认：默认自动 Ack；对复杂事务改为 `WithManualAck()`
4. 重试：`WithRetry`（有限重试 + 指数退避）
5. 限流与背压：
   - Redis：`WithBatchSize(50~200)` 压测定型
   - JetStream：`WithMaxInflight(100~1000)` 按处理能力调节
6. 死信：业务层显式发布 `*.dlq`，并配套监控告警

这套基线可以在当前版本直接落地，不依赖尚未实现的 Kafka/DLQ 自动化能力。
