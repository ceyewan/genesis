# mq - 消息队列组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/mq.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/mq)

`mq` 是 Genesis 的 L2 业务层组件，提供统一的发布订阅接入方式，但不把不同后端伪装成完全一致的语义。当前支持两种持久化后端：

- **NATS JetStream**：持久化流式系统，支持显式 Ack/Nak、durable consumer 和消息重投。
- **Redis Stream**：基于 Consumer Group，复用现有 Redis 设施，Nak 语义不同（见下文）。

接口设计与取舍详见 [genesis-mq-blog.md](../docs/genesis-mq-blog.md)，完整 API 文档见 `go doc ./mq`。

## 快速开始

### NATS JetStream

```go
natsConn, err := connector.NewNATS(&connector.NATSConfig{
    URL: "nats://localhost:4222",
})
if err != nil {
    return err
}
defer natsConn.Close()
if err := natsConn.Connect(ctx); err != nil {
    return err
}

q, err := mq.New(&mq.Config{
    Driver: mq.DriverNATSJetStream,
    JetStream: &mq.JetStreamConfig{
        AutoCreateStream: true,
    },
}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))
if err != nil {
    return err
}
defer q.Close()

sub, err := q.Subscribe(ctx, "orders.created", func(msg mq.Message) error {
    return processOrder(msg.Data())
}, mq.WithQueueGroup("order-workers"), mq.WithAutoAck())
if err != nil {
    return err
}
defer sub.Unsubscribe()

if err := q.Publish(ctx, "orders.created", []byte(`{"id": 123}`),
    mq.WithHeader("trace-id", "abc123")); err != nil {
    return err
}
```

JetStream 下 `Publish` 只有在 broker 返回 `PubAck` 后才返回 nil；它不是“只写入客户端 socket 即成功”。退出时可用 `q.Drain(ctx)` 停止新投递并等待已交付 Handler 完成。`Close()` 使用 5 秒上限强制停止，适合作为兜底清理。
开启 `AutoCreateStream` 后，发布端和订阅端都会在首次使用 topic 时确保 Stream 存在；生产环境仍建议关闭并由运维预创建。

### Redis Stream

```go
redisConn, err := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
})
if err != nil {
    return err
}
defer redisConn.Close()
if err := redisConn.Connect(ctx); err != nil {
    return err
}

q, err := mq.New(&mq.Config{
    Driver: mq.DriverRedisStream,
    RedisStream: &mq.RedisStreamConfig{
        MaxLen: 100000,
    },
}, mq.WithRedisConnector(redisConn), mq.WithLogger(logger))
if err != nil {
    return err
}
defer q.Close()

sub, err := q.Subscribe(ctx, "events", handler,
    mq.WithQueueGroup("event-processors"),
    mq.WithDurable("worker-1"),
    mq.WithBatchSize(50))
if err != nil {
    return err
}
defer sub.Unsubscribe()
```

## Ack/Nak 语义

| 操作 | JetStream | Redis Stream |
|------|-----------|-------------|
| `Ack()` | 发送 Ack 到服务端，消息从 pending 移除 | 执行 `XACK` |
| `Nak()` | 触发消息立即重投 | 返回 `ErrNotSupported`；消息留在 Pending，由 `XAUTOCLAIM` 超时后重认领 |
| `NakWithDelay(d)` | 延迟 `d` 后重投 | 返回 `ErrNotSupported` |

**默认是手动确认**（ManualAck）。`WithAutoAck()` 开启后，Handler 返回 nil 时调用 Ack，只有 Ack 也成功才记录消费成功；Ack 失败会返回错误并记录失败。Handler 返回 error 时自动调用 Nak；Redis 下的 `ErrNotSupported` 会被静默忽略，不记录为额外错误。

### 长任务心跳

NATS JetStream 消息还提供可选的 `ProgressMessage` 能力。长任务可在处理期间定期调用 `InProgress()` 重置服务端 `AckWait`，避免任务仍在执行时被提前重投：

```go
if progress, ok := msg.(mq.ProgressMessage); ok {
    if err := progress.InProgress(); err != nil {
        return err
    }
}
```

`ProgressMessage` 没有加入基础 `Message` 接口；Redis Stream 消息不实现它。跨驱动代码必须使用类型断言。实际长任务应按小于 `AckWait` 的周期持续发送心跳，并在 Handler 返回前停止心跳 goroutine。

## 订阅选项

| 选项 | 描述 | 驱动支持 |
|------|------|----------|
| `WithQueueGroup(name)` | 消费组，多实例竞争消费 | JetStream: 按 topic 隔离的 durable consumer 逻辑名；Redis: consumer group 名 |
| `WithAutoAck()` | 开启自动确认 | 两者 |
| `WithManualAck()` | 手动确认（默认） | 两者 |
| `WithDurable(name)` | 消费者实例名 | JetStream: 按 topic 隔离的 durable consumer 逻辑名（QueueGroup 为空时）；Redis: consumer name |
| `WithBatchSize(n)` | 单次拉取/本地预取上限，默认 10 | Redis 对应读取 `COUNT`；JetStream 显式设置时对应实例级 `PullMaxMessages` |
| `WithMaxInflight(n)` | 共享 durable 的集群级未确认总数 | JetStream 对应 `MaxAckPending`；Redis 无对应 |
| `FromBeginning()` | 新建 consumer 从保留消息起点消费（默认） | 两者 |
| `FromLatest()` | 新建 consumer 只消费订阅后到达的消息 | 两者 |
| `FromID(id)` | 从显式后端位置开始；JetStream 使用 Genesis sequence ID | 两者，ID 格式不同 |

## 中间件

```go
handler = mq.Chain(
    mq.WithRecover(logger),                          // 最外层：捕获 panic
    mq.WithLogging(logger),                          // 记录每条消息的处理结果
    mq.WithRetry(mq.DefaultRetryConfig(), logger),   // 内层：指数退避重试
)(businessHandler)
```

内置中间件：`WithRetry`、`WithLogging`、`WithRecover`、`WithDeadLetter`。
`RetryConfig.Multiplier` 必须是大于 1 的有限数值；NaN、无穷或不大于 1 的值会归一化为默认值 `2.0`，避免退避时间溢出或退化成即时重试。

## 配置

### JetStreamConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `AutoCreateStream` | `bool` | `false` | 自动建 Stream（生产环境建议关闭） |
| `StreamPrefix` | `string` | `"S-"` | Stream 名称前缀 |
| `AckWait` | `time.Duration` | `30s` | Ack 超时，超时后消息自动重投，建议设为最大处理时间的 2 倍 |
| `MaxDeliver` | `int` | `5` | 最大投递次数；业务 DLQ 主题仍由应用定义 |
| `Retention` | `StreamRetention` | `limits` | 新建 Stream 的 limits / interest / work_queue 策略 |
| `Storage` | `StreamStorage` | `file` | 新建 Stream 的 file / memory 存储 |
| `MaxAge` | `time.Duration` | `0`（不限） | 新建 Stream 的消息最长保留时间 |
| `MaxBytes` | `int64` | `0`（服务端不限） | 新建 Stream 的最大字节数 |
| `Replicas` | `int` | `1` | 新建 Stream 的副本数，范围 1–5 |

这些 Stream 字段只用于 Genesis 自动创建的新 Stream。若 Stream 已存在，Genesis 只在需要时补充 subject，不覆盖其运维配置。生产环境建议关闭 `AutoCreateStream` 并预建 Stream。

### RedisStreamConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `MaxLen` | `int64` | `0`（不限） | Stream 最大长度，超过后裁剪旧消息 |
| `Approximate` | `bool` | `false` | 近似裁剪（`MAXLEN ~`），性能更好但不精确 |
| `PendingIdle` | `time.Duration` | `30s` | Pending 消息空闲超时，超时后可被其他消费者认领 |

## 错误与生命周期

```go
var (
    ErrClosed             // Close 后调用 Publish/Subscribe 时返回
    ErrNotSupported       // 驱动不支持的操作（如 Redis 的 Nak）
    ErrInvalidConfig      // 配置校验失败
    ErrPanicRecovered     // WithRecover 捕获到 panic
)
```

空 Driver、空 topic 和 nil Handler 都返回可由 `errors.Is(err, ErrInvalidConfig)` 判断的错误。

`Drain(ctx)` 会停止新投递并等待已交付 Handler 完成；ctx 到期后强制停止。`Close()` 使用 5 秒默认上限取消已有订阅并等待退出。两者都可并发重复调用，生命周期开始关闭后 `Publish` 和 `Subscribe` 返回 `ErrClosed`。MQ 只借用注入的 Connector；关闭 MQ 不会关闭 Connector，连接生命周期仍由调用方负责。

## 测试

```bash
go test ./mq/... -count=1
go test -race ./mq/... -count=1
```

集成测试通过 testcontainers 自动启动 NATS 和 Redis 容器，直接运行即可，无需手动执行 `make up`。

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/mq)
- [组件设计博客](../docs/genesis-mq-blog.md)
- [Genesis 文档目录](../docs/README.md)
