# mq - Genesis 消息队列组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/mq.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/mq)

`mq` 是 Genesis 业务层的消息队列抽象组件，提供统一的发布订阅 API，支持 NATS Core、NATS JetStream、Redis Stream 等多种后端实现。

## 设计理念

- **简单优于复杂**：核心接口精简，通过 Option 扩展能力
- **显式优于隐式**：不做自动注入，用户完全掌控消息流
- **可扩展性**：Transport 接口设计兼顾未来 Kafka 等重量级 MQ

## 支持的后端

| 驱动 | 说明 | 持久化 | 消息确认 | 队列组 |
|------|------|--------|----------|--------|
| `nats_core` | NATS Core（高性能） | ❌ | ❌ | ✅ |
| `nats_jetstream` | NATS JetStream | ✅ | ✅ | ✅ |
| `redis_stream` | Redis Stream | ✅ | ✅ | ✅ |
| `kafka` | Kafka（预留） | - | - | - |

## 特性对比

| 特性 | NATS Core | JetStream | Redis Stream |
|------|-----------|-----------|--------------|
| 持久化 | ❌ | ✅ | ✅ |
| 消息确认 (Ack) | ❌ | ✅ | ✅ |
| 消息拒绝 (Nak) | ❌ | ✅ | ❌* |
| 队列组 | ✅ | ✅ | ✅ |
| 批量消费 | ❌ | ✅ | ✅ |
| 最大在途限制 | ❌ | ✅ | ❌ |
| 持久化订阅 | ❌ | ✅ | ✅ |

*Redis Stream 无原生 Nak，消息留在 Pending 列表可被 XCLAIM

## 快速开始

### 安装

```bash
go get github.com/ceyewan/genesis/mq
```

### NATS JetStream 示例

```go
package main

import (
    "context"

    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/mq"
)

func main() {
    ctx := context.Background()

    // 1. 创建 NATS 连接
    natsConn, _ := connector.NewNATS(&connector.NATSConfig{
        URLs: []string{"nats://localhost:4222"},
    })
    _ = natsConn.Connect(ctx)
    defer natsConn.Close()

    // 2. 创建 MQ 实例
    mq, _ := mq.New(&mq.Config{
        Driver: mq.DriverNATSJetStream,
        JetStream: &mq.JetStreamConfig{
            AutoCreateStream: true,
        },
    }, mq.WithNATSConnector(natsConn))
    defer mq.Close()

    // 3. 订阅消息
    sub, _ := mq.Subscribe(ctx, "orders.created", func(msg mq.Message) error {
        // 处理消息，返回 nil 自动 Ack
        return processOrder(msg.Data())
    }, mq.WithQueueGroup("order-workers"))

    // 4. 发布消息
    _ = mq.Publish(ctx, "orders.created", []byte(`{"id": 123}`),
        mq.WithHeader("trace-id", "abc123"))

    // 5. 等待订阅结束
    <-sub.Done()
}

func processOrder(data []byte) error {
    // 业务逻辑
    return nil
}
```

### Redis Stream 示例

```go
// 创建 Redis 连接
redisConn, _ := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
})
_ = redisConn.Connect(ctx)
defer redisConn.Close()

// 创建 MQ 实例
mq, _ := mq.New(&mq.Config{
    Driver: mq.DriverRedisStream,
    RedisStream: &mq.RedisStreamConfig{
        MaxLen: 10000,
    },
}, mq.WithRedisConnector(redisConn))
defer mq.Close()

// 订阅（Consumer Group 模式）
mq.Subscribe(ctx, "events", handler,
    mq.WithQueueGroup("event-processors"),
    mq.WithDurable("worker-1"),
    mq.WithBatchSize(50),
)
```

## 核心接口

### MQ

消息队列核心接口，提供发布订阅能力。

```go
type MQ interface {
    // 发布消息
    Publish(ctx context.Context, topic string, data []byte, opts ...PublishOption) error

    // 订阅消息
    Subscribe(ctx context.Context, topic string, handler Handler, opts ...SubscribeOption) (Subscription, error)

    // 关闭客户端
    Close() error
}
```

### Message

消息接口，提供统一的数据访问和确认机制。

```go
type Message interface {
    Context() context.Context  // 获取处理上下文
    Topic() string              // 获取主题
    Data() []byte               // 获取消息体
    Headers() Headers           // 获取消息头
    Ack() error                 // 确认消息
    Nak() error                 // 拒绝消息
    ID() string                 // 获取消息ID
}
```

### Handler

消息处理函数，只接收 Message 参数，通过 `msg.Context()` 获取上下文。

```go
type Handler func(msg Message) error
```

**设计说明**：去掉冗余的 ctx 参数，避免 ctx 和 msg.Context() 同时存在造成的困惑。

### Subscription

订阅句柄，用于管理订阅生命周期。

```go
type Subscription interface {
    Unsubscribe() error           // 取消订阅
    Done() <-chan struct{}        // 订阅结束时关闭
}
```

## 配置选项

### 创建 MQ

```go
mq, err := mq.New(&mq.Config{
    Driver: mq.DriverNATSJetStream,
    JetStream: &mq.JetStreamConfig{
        AutoCreateStream: true,
        StreamPrefix:     "S-",
    },
},
    mq.WithLogger(logger),      // 注入日志器
    mq.WithMeter(meter),        // 注入指标收集器
    mq.WithNATSConnector(conn), // 注入连接器
)
```

### 发布选项

```go
mq.Publish(ctx, "topic", data,
    mq.WithHeaders(mq.Headers{"trace-id": "abc123"}),  // 设置消息头
    mq.WithHeader("key", "value"),                      // 设置单个消息头
    mq.WithKey("routing-key"),                          // 设置路由键（预留）
)
```

### 订阅选项

```go
mq.Subscribe(ctx, "topic", handler,
    mq.WithQueueGroup("workers"),     // 队列组（负载均衡）
    mq.WithManualAck(),               // 关闭自动确认，手动调用 msg.Ack()
    mq.WithAutoAck(),                 // 开启自动确认（默认）
    mq.WithDurable("durable-1"),      // 持久化订阅名（JetStream/Redis）
    mq.WithBatchSize(50),             // 批量拉取大小（默认 10）
    mq.WithMaxInflight(100),          // 最大在途消息数（JetStream）
    mq.WithBufferSize(100),           // 内部缓冲区大小（默认 100）
)
```

## 中间件

mq 提供中间件机制增强 Handler，支持链式组合。

### 内置中间件

```go
// 重试中间件
retryHandler := mq.WithRetry(mq.DefaultRetryConfig, logger)(handler)

// 日志中间件
loggedHandler := mq.WithLogging(logger)(handler)

// Panic 恢复中间件
safeHandler := mq.WithRecover(logger)(handler)
```

### 链式组合

执行顺序：第一个中间件最先执行，最后一个最接近原始 Handler。

```go
handler = mq.Chain(
    mq.WithRecover(logger),            // 最外层：捕获 panic
    mq.WithLogging(logger),            // 中间层：记录日志
    mq.WithRetry(mq.DefaultRetryConfig, logger), // 内层：重试
)(handler)

// 执行顺序：WithRecover -> WithLogging -> WithRetry -> handler
```

### 重试配置

```go
type RetryConfig struct {
    MaxRetries     int           // 最大重试次数（不含首次）
    InitialBackoff time.Duration // 初始退避时间
    MaxBackoff     time.Duration // 最大退避时间
    Multiplier     float64       // 退避倍数
}

// 默认配置
var DefaultRetryConfig = RetryConfig{
    MaxRetries:     3,
    InitialBackoff: 100 * time.Millisecond,
    MaxBackoff:     5 * time.Second,
    Multiplier:     2.0,
}
```

## 指标

| 指标名 | 类型 | 描述 |
|--------|------|------|
| `mq.publish.total` | Counter | 发布消息总数 |
| `mq.publish.duration` | Histogram | 发布延迟 (秒) |
| `mq.consume.total` | Counter | 消费消息总数 |
| `mq.handle.duration` | Histogram | 处理耗时 (秒) |

标签：`topic`、`status`、`driver`

## 错误处理

预定义错误常量：

```go
var (
    ErrClosed              = xerrors.New("mq: client closed")
    ErrInvalidConfig       = xerrors.New("mq: invalid config")
    ErrNotSupported        = xerrors.New("mq: operation not supported by this driver")
    ErrSubscriptionClosed  = xerrors.New("mq: subscription closed")
    ErrPanicRecovered      = xerrors.New("mq: handler panic recovered")
)
```

## 能力检查

不同后端能力差异较大，可根据配置时的 Driver 判断：

```go
if cfg.Driver == mq.DriverNATSCore {
    // NATS Core 不支持持久化，不要用于关键业务
}

if cfg.Driver == mq.DriverNATSJetStream {
    // JetStream 支持 Ack/Nak，可配合 WithManualAck 使用
}
```

## 配置详情

### JetStream 配置

```go
type JetStreamConfig struct {
    AutoCreateStream bool   // 是否自动创建 Stream
    StreamPrefix     string // Stream 名称前缀，默认 "S-"
}
```

### Redis Stream 配置

```go
type RedisStreamConfig struct {
    MaxLen      int64  // Stream 最大长度，0 表示不限制
    Approximate bool   // 是否使用近似裁剪（性能更好）
}
```

## 最佳实践

### 1. 手动确认模式

```go
mq.Subscribe(ctx, "topic", func(msg mq.Message) error {
    if err := process(msg.Data()); err != nil {
        msg.Nak() // 拒绝消息，触发重投
        return err
    }
    msg.Ack() // 确认消息
    return nil
}, mq.WithManualAck())
```

### 2. 中间件组合

```go
// 推荐顺序：Recover -> Logging -> Retry -> Handler
handler = mq.Chain(
    mq.WithRecover(logger),
    mq.WithLogging(logger),
    mq.WithRetry(mq.DefaultRetryConfig, logger),
)(businessHandler)
```

### 3. 上下文传递

```go
handler := func(msg mq.Message) error {
    ctx := msg.Context() // 获取订阅时的上下文

    // 业务逻辑使用 msg.Context()，而非 Subscribe 时的 ctx
    return processOrder(ctx, msg.Data())
}
```

## 测试

```bash
# 运行单元测试
go test ./mq/...

# 运行集成测试（需要本地环境）
make up
go test ./mq/... -tags=integration
make down
```

## 未来规划

- [ ] Kafka 支持
- [ ] 死信队列实现
- [ ] 延迟消息支持
- [ ] 事务消息（Kafka）
