# mq - 消息队列组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/mq.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/mq)

`mq` 是 Genesis 的 L2 业务层组件，提供统一的发布订阅接入方式，但不把不同后端伪装成完全一致的语义。当前支持两种持久化后端：

- **NATS JetStream**：持久化流式系统，支持显式 Ack/Nak、durable consumer 和消息重投。
- **Redis Stream**：基于 Consumer Group，复用现有 Redis 设施，Nak 语义不同（见下文）。

接口设计与取舍详见 [genesis-mq-blog.md](../docs/genesis-mq-blog.md)，完整 API 文档见 `go doc ./mq`。

## 快速开始

### NATS JetStream

```go
natsConn, _ := connector.NewNATS(&connector.NATSConfig{
    URLs: []string{"nats://localhost:4222"},
})
_ = natsConn.Connect(ctx)
defer natsConn.Close()

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

_ = q.Publish(ctx, "orders.created", []byte(`{"id": 123}`),
    mq.WithHeader("trace-id", "abc123"))
```

### Redis Stream

```go
redisConn, _ := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
})
_ = redisConn.Connect(ctx)
defer redisConn.Close()

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

sub, _ := q.Subscribe(ctx, "events", handler,
    mq.WithQueueGroup("event-processors"),
    mq.WithDurable("worker-1"),
    mq.WithBatchSize(50))
defer sub.Unsubscribe()
```

## Ack/Nak 语义

| 操作 | JetStream | Redis Stream |
|------|-----------|-------------|
| `Ack()` | 发送 Ack 到服务端，消息从 pending 移除 | 执行 `XACK` |
| `Nak()` | 触发消息立即重投 | 返回 `ErrNotSupported`；消息留在 Pending，由 `XAUTOCLAIM` 超时后重认领 |

**默认是手动确认**（ManualAck）。`WithAutoAck()` 开启后，Handler 返回 error 自动调用 Nak；Redis 下的 `ErrNotSupported` 会被静默忽略，不记录为错误。

## 订阅选项

| 选项 | 描述 | 驱动支持 |
|------|------|----------|
| `WithQueueGroup(name)` | 消费组，多实例竞争消费 | JetStream: durable consumer 名；Redis: consumer group 名 |
| `WithAutoAck()` | 开启自动确认 | 两者 |
| `WithManualAck()` | 手动确认（默认） | 两者 |
| `WithDurable(name)` | 消费者实例名 | JetStream: durable consumer 名（QueueGroup 为空时）；Redis: consumer name |
| `WithBatchSize(n)` | 单次拉取大小，默认 10 | Redis 有效；JetStream 当前无效（push 模式） |
| `WithMaxInflight(n)` | 最大在途消息数 | JetStream 对应 `MaxAckPending`；Redis 无对应 |

## 中间件

```go
handler = mq.Chain(
    mq.WithRecover(logger),                          // 最外层：捕获 panic
    mq.WithLogging(logger),                          // 记录每条消息的处理结果
    mq.WithRetry(mq.DefaultRetryConfig, logger),     // 内层：指数退避重试
)(businessHandler)
```

内置中间件：`WithRetry`、`WithLogging`、`WithRecover`、`WithDeadLetter`。

## 配置

### JetStreamConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `AutoCreateStream` | `bool` | `false` | 自动建 Stream（生产环境建议关闭） |
| `StreamPrefix` | `string` | `"S-"` | Stream 名称前缀 |
| `AckWait` | `time.Duration` | `30s` | Ack 超时，超时后消息自动重投，建议设为最大处理时间的 2 倍 |

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
    ErrSubscriptionClosed // 订阅已关闭
    ErrPanicRecovered     // WithRecover 捕获到 panic
)
```

`Close()` 是幂等操作，多次调用不报错。关闭后 `Publish` 和 `Subscribe` 返回 `ErrClosed`，可通过 `errors.Is` 检测。

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
