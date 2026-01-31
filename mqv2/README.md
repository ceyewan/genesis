# mqv2 - Genesis 消息队列组件 (v2)

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/mqv2.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/mqv2)

`mqv2` 是 Genesis 业务层的消息队列抽象组件，提供统一的发布订阅 API，支持多种后端实现。

## 设计理念

- **简单优于复杂**：核心接口精简，通过 Option 扩展能力
- **显式优于隐式**：不做自动注入，用户完全掌控消息流
- **可扩展性**：Transport 接口设计兼顾未来 Kafka 等重量级 MQ

## 特性

| 特性 | NATS Core | JetStream | Redis Stream | Kafka (预留) |
|------|-----------|-----------|--------------|--------------|
| 持久化 | ❌ | ✅ | ✅ | ✅ |
| 消息确认 | ❌ | ✅ | ✅ | ✅ |
| 消息拒绝 (Nak) | ❌ | ✅ | ❌ | ❌ |
| 队列组 | ✅ | ✅ | ✅ | ✅ |
| 顺序保证 | ❌ | ✅* | ✅ | ✅* |
| 批量消费 | ❌ | ✅ | ✅ | ✅ |
| 死信队列 | ❌ | 预留 | 预留 | 预留 |

*单消费者/单分区时保证顺序

## 快速开始

### 安装

```bash
go get github.com/ceyewan/genesis/mqv2
```

### NATS JetStream 示例

```go
package main

import (
    "context"
    "log"

    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/mqv2"
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
    mq, _ := mqv2.New(&mqv2.Config{
        Driver: mqv2.DriverNATSJetStream,
        JetStream: &mqv2.JetStreamConfig{
            AutoCreateStream: true,
        },
    }, mqv2.WithNATSConnector(natsConn))
    defer mq.Close()

    // 3. 订阅消息
    sub, _ := mq.Subscribe(ctx, "orders.created", func(msg mqv2.Message) error {
        log.Printf("Received: %s", msg.Data())
        return nil // 返回 nil 自动 Ack
    }, mqv2.WithQueueGroup("order-workers"))

    // 4. 发布消息
    _ = mq.Publish(ctx, "orders.created", []byte(`{"id": 123}`),
        mqv2.WithHeader("trace-id", "abc123"))

    // 5. 清理
    <-sub.Done()
}
```

### Redis Stream 示例

```go
// 创建 Redis 连接
redisConn, _ := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
})
_ = redisConn.Connect(ctx)

// 创建 MQ 实例
mq, _ := mqv2.New(&mqv2.Config{
    Driver: mqv2.DriverRedisStream,
}, mqv2.WithRedisConnector(redisConn))

// 订阅（Consumer Group 模式）
mq.Subscribe(ctx, "events", handler, 
    mqv2.WithQueueGroup("event-processors"),
    mqv2.WithDurable("worker-1"))
```

## Handler 设计

`Handler` 只接收 `Message` 参数，通过 `msg.Context()` 获取上下文：

```go
handler := func(msg mqv2.Message) error {
    ctx := msg.Context() // 获取上下文
    
    // 业务逻辑
    return processOrder(ctx, msg.Data())
}
```

**为什么这样设计？**

旧版 API `func(ctx, msg)` 存在困惑：ctx 和 msg.Context() 哪个才是"对的"？
新版统一从 msg.Context() 获取，语义清晰。

## 订阅选项

```go
mq.Subscribe(ctx, "topic", handler,
    // 队列组（负载均衡）
    mqv2.WithQueueGroup("workers"),
    
    // 关闭自动确认，手动调用 msg.Ack()
    mqv2.WithManualAck(),
    
    // 持久化订阅名（JetStream/Redis）
    mqv2.WithDurable("durable-1"),
    
    // 批量拉取大小
    mqv2.WithBatchSize(50),
    
    // 最大在途消息数（JetStream）
    mqv2.WithMaxInflight(100),
    
    // 死信队列（预留，暂未实现）
    mqv2.WithDeadLetter(3, "dead-letter-topic"),
)
```

## 中间件

mqv2 提供中间件机制增强 Handler：

```go
// 重试中间件
retryHandler := mqv2.WithRetry(mqv2.DefaultRetryConfig, logger)(handler)

// 日志中间件
loggedHandler := mqv2.WithLogging(logger)(handler)

// Panic 恢复
safeHandler := mqv2.WithRecover(logger)(handler)

// 串联多个中间件
handler = mqv2.Chain(
    mqv2.WithRecover(logger),
    mqv2.WithLogging(logger),
    mqv2.WithRetry(mqv2.DefaultRetryConfig, logger),
)(handler)
```

## 能力检查

不同后端能力差异较大，可通过 `Capabilities` 运行时检查：

```go
// 获取 Transport 能力（需要类型断言访问内部方法）
// 或通过配置时的 Driver 判断

if cfg.Driver == mqv2.DriverNATSCore {
    // NATS Core 不支持持久化，不要用于关键业务
}
```

## 指标

| 指标名 | 类型 | 描述 |
|--------|------|------|
| `mq.publish.total` | Counter | 发布消息总数 |
| `mq.publish.duration` | Histogram | 发布延迟 (秒) |
| `mq.consume.total` | Counter | 消费消息总数 |
| `mq.handle.duration` | Histogram | 处理耗时 (秒) |

标签：`topic`, `status`

## 迁移指南 (v1 → v2)

| v1 | v2 | 说明 |
|----|-----|------|
| `mq.Client` | `mqv2.MQ` | 接口重命名 |
| `Handler(ctx, msg)` | `Handler(msg)` | 去掉冗余 ctx 参数 |
| `msg.Subject()` | `msg.Topic()` | 统一术语 |
| `DriverNatsJetStream` | `DriverNATSJetStream` | 命名规范化 |
| `DriverRedis` | `DriverRedisStream` | 明确是 Stream |
| `SubscribeChan` | 已移除 | 使用 Handler 模式 |

## 未来规划

- [ ] Kafka 支持
- [ ] 死信队列实现
- [ ] 延迟消息支持
- [ ] 事务消息（Kafka）
