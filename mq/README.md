# mq - Genesis 消息队列组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/mq.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/mq)

`mq` 是 Genesis 业务层的核心组件，提供统一的消息队列抽象。它通过 Driver 模式支持多种底层实现（NATS, Redis），并提供统一的发布订阅 API。

## 特性

- **所属层级**：L2 (Business)
- **多驱动支持**：
    - **NATS**: 支持 Core (高性能) 和 JetStream (持久化)
    - **Redis**: 支持 Redis Stream (持久化队列)
- **统一抽象**：屏蔽底层差异，提供一致的 `Publish/Subscribe` 接口
- **增强功能**：
    - **Channel 模式**：支持 Go Channel 风格的消息消费 (`SubscribeChan`)
    - **重试机制**：内置指数退避重试中间件 (`WithRetry`)
    - **Options 模式**：灵活配置队列组、缓冲区、自动确认等
- **可观测性**：集成 clog 和 metrics

## 目录结构

```text
mq/
├── mq.go                  # Client 接口定义
├── client.go              # Client 通用实现
├── driver.go              # 驱动内部接口定义
├── driver_nats.go         # NATS 驱动
├── driver_redis.go        # Redis 驱动
├── options.go             # 订阅选项
├── retry.go               # 重试中间件
├── types.go               # 类型与配置定义
└── README.md              # 本文档
```

## 快速开始

### 1. NATS (JetStream)

```go
// 创建 Connector
natsConn, _ := connector.NewNATS(&cfg.NATS, connector.WithLogger(logger))
natsConn.Connect(ctx)

// 创建 Client
client, _ := mq.New(&mq.Config{
    Driver: mq.DriverNatsJetStream,
    JetStream: &mq.JetStreamConfig{
        AutoCreateStream: true,
    },
}, mq.WithNATSConnector(natsConn), mq.WithLogger(logger))

// 订阅 (Queue Group 负载均衡)
client.Subscribe(ctx, "orders.created", handler, mq.WithQueueGroup("order_workers"))
```

### 2. Redis Stream

```go
// 创建 Connector
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
redisConn.Connect(ctx)

// 创建 Client
client, _ := mq.New(&mq.Config{
    Driver: mq.DriverRedis,
}, mq.WithRedisConnector(redisConn), mq.WithLogger(logger))

// 订阅
client.Subscribe(ctx, "orders.created", handler, mq.WithQueueGroup("order_workers"))
```

## 高级功能

### Channel 模式

适合高吞吐处理或习惯 Go Channel 的场景：

```go
ch, sub, err := client.SubscribeChan(ctx, "events", mq.WithBufferSize(100))
defer sub.Unsubscribe()

for msg := range ch {
    process(msg)
    msg.Ack() // 手动确认
}
```

### 重试中间件

为 Handler 增加自动重试能力：

```go
handler := func(ctx context.Context, msg mq.Message) error {
    // 业务逻辑...
    return err // 返回错误触发重试
}

// 包装 Handler：最大重试 3 次
client.Subscribe(ctx, "topic", mq.WithRetry(mq.DefaultRetryConfig, logger)(handler))
```

### 订阅选项

```go
client.Subscribe(ctx, "topic", handler,
    mq.WithQueueGroup("group1"), // 负载均衡组
    mq.WithManualAck(),          // 关闭自动 Ack
    mq.WithDurable("durable1"),  // 持久化订阅名 (JetStream/Redis)
    mq.WithBatchSize(50),        // 批量拉取大小 (Redis)
    mq.WithMaxInflight(100),     // 最大在途消息数 (JetStream)
    mq.WithAsyncAck(),           // 开启异步确认 (提升吞吐)
    mq.WithDeadLetter(3, "dlq"), // 设置死信队列 (3次失败后转发到 dlq)
)
```

## 接口设计

### 工厂入口

```go
client, _ := mq.New(&mq.Config{
    Driver: mq.DriverNatsCore,
}, mq.WithNATSConnector(natsConn))
```

### Client 接口

对外暴露的统一 API：

```go
type Client interface {
    Publish(ctx context.Context, subject string, data []byte, opts ...PublishOption) error
    Subscribe(ctx context.Context, subject string, handler Handler, opts ...SubscribeOption) (Subscription, error)
    SubscribeChan(ctx context.Context, subject string, opts ...SubscribeOption) (<-chan Message, Subscription, error)
    Close() error
}
```
