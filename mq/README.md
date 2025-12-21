# mq - Genesis 消息队列组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/mq.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/mq)

`mq` 是 Genesis 业务层的核心组件，提供基于 NATS 的消息队列能力，支持 Core（即时通信）和 JetStream（持久化流）两种模式。

## 特性

- **所属层级**：L2 (Business) — 业务能力，提供消息队列抽象
- **核心职责**：在 NATS 连接器的基础上提供统一的消息发布订阅语义
- **设计原则**：
    - **双模式支持**：Core 模式（高吞吐、低延迟、发后即忘）和 JetStream 模式（持久化、可靠投递）
    - **统一抽象**：屏蔽底层驱动差异，提供一致的 `Publish/Subscribe` 接口
    - **借用模型**：借用 NATS 连接器的连接，不负责连接的生命周期
    - **灵活订阅**：支持广播订阅（Subscribe）和队列订阅（QueueSubscribe）
    - **可观测性**：集成 clog 和 metrics，提供完整的日志和指标能力
    - **自动重试**：JetStream 模式下支持消息重试和死信队列

## 目录结构（完全扁平化设计）

```text
mq/                        # 公开 API + 实现（完全扁平化）
├── README.md              # 本文档
├── mq.go                  # Client 接口和实现，New 构造函数
├── config.go              # 配置结构：Config + JetStreamConfig
├── options.go             # 函数式选项：Option、WithLogger/WithMeter
├── impl.go                # Core 和 JetStream 的具体实现
└── *_test.go              # 测试文件
```

**设计原则**：完全扁平化设计，所有公开 API 和实现都在根目录，无 `types/` 子包

## 快速开始

```go
import "github.com/ceyewan/genesis/mq"
```

### 基础使用

```go
// 1. 创建连接器
natsConn, _ := connector.NewNATS(&cfg.NATS, connector.WithLogger(logger))
defer natsConn.Close()
natsConn.Connect(ctx)

// 2. 创建 MQ 客户端
mqClient, _ := mq.New(natsConn, &mq.Config{
    Driver: mq.DriverNatsJetStream,
    JetStream: &mq.JetStreamConfig{
        AutoCreateStream: true,
    },
}, mq.WithLogger(logger))

// 3. 发布消息
err := mqClient.Publish(ctx, "orders.created", orderData)

// 4. 订阅消息
sub, _ := mqClient.QueueSubscribe(ctx, "orders.created", "order_processors", func(ctx context.Context, msg mq.Message) error {
    // 处理订单
    return nil
})
defer sub.Unsubscribe()
```

## 模式选择

### Core 模式 vs JetStream 模式

| 特性       | Core 模式            | JetStream 模式         |
| ---------- | -------------------- | ---------------------- |
| **性能**   | 极高延迟 < 1ms       | 高延迟 < 5ms           |
| **可靠性** | 发后即忘，可能丢消息 | 持久化，不丢消息       |
| **消费者** | 在线消费者才能收到   | 支持离线消息，重投机制 |
| **用例**   | 实时指标、日志、通知 | 订单处理、状态机流转   |

### Core 模式示例

```go
// Core 模式：高吞吐、低延迟
mqClient, _ := mq.New(natsConn, &mq.Config{
    Driver: mq.DriverNatsCore,
}, mq.WithLogger(logger))

// 发布实时指标
err := mqClient.Publish(ctx, "metrics.cpu", cpuData)

// 广播配置更新
mqClient.Subscribe(ctx, "config.updates", func(ctx context.Context, msg mq.Message) error {
    applyConfig(msg.Data())
    return nil
})
```

### JetStream 模式示例

```go
// JetStream 模式：持久化、可靠
mqClient, _ := mq.New(natsConn, &mq.Config{
    Driver: mq.DriverNatsJetStream,
    JetStream: &mq.JetStreamConfig{
        AutoCreateStream: true,
    },
}, mq.WithLogger(logger))

// 发布订单事件
err := mqClient.Publish(ctx, "orders.created", orderData)

// 队列订阅处理订单（负载均衡）
sub, _ := mqClient.QueueSubscribe(ctx, "orders.created", "order_workers", func(ctx context.Context, msg mq.Message) error {
    var order Order
    json.Unmarshal(msg.Data(), &order)

    // 处理订单逻辑
    if err := processOrder(order); err != nil {
        return err // 返回错误会自动触发重试
    }

    return nil // 返回 nil 会自动 Ack
})
```

## 核心接口

### Client 接口

```go
type Client interface {
    // Publish 发布消息
    // Core 模式：发后即忘，极高性能
    // JetStream 模式：等待持久化确认，高可靠性
    Publish(ctx context.Context, subject string, data []byte) error

    // Subscribe 广播订阅
    // 所有订阅该 Subject 的消费者都会收到消息
    // 适用于：配置更新通知、缓存失效通知
    Subscribe(ctx context.Context, subject string, handler Handler) (Subscription, error)

    // QueueSubscribe 队列订阅（负载均衡）
    // 同一个 queue 组内的消费者，每条消息只会被其中一个处理
    // 适用于：任务分发、订单处理
    QueueSubscribe(ctx context.Context, subject string, queue string, handler Handler) (Subscription, error)


    // Close 关闭客户端
    Close() error
}
```

### Message 接口

```go
type Message interface {
    // Subject 获取消息主题
    Subject() string

    // Data 获取消息内容
    Data() []byte

    // Ack 确认消息处理成功（仅 JetStream 模式有效）
    // Core 模式下为空操作
    Ack() error

    // Nak 否认消息，请求重投（仅 JetStream 模式有效）
    Nak() error
}
```

### Handler 处理函数

```go
// Handler 消息处理函数
// 返回 nil 表示处理成功，JetStream 模式会自动 Ack
// 返回 error 表示处理失败，JetStream 模式会自动 Nak 并重试
type Handler func(ctx context.Context, msg Message) error
```

## 配置设计

### Config 结构

```go
type Config struct {
    // 驱动类型：nats_core 或 nats_jetstream
    Driver DriverType `json:"driver" yaml:"driver"`

    // JetStream 特有配置（仅当 Driver 为 nats_jetstream 时有效）
    JetStream *JetStreamConfig `json:"jetstream" yaml:"jetstream"`
}
```

### JetStreamConfig 结构

```go
type JetStreamConfig struct {
    // 是否自动创建 Stream（如果不存在）
    AutoCreateStream bool `json:"auto_create_stream" yaml:"auto_create_stream"`
}
```

## 使用模式

### 1. 生产者-消费者模式（推荐）

这是最常用的异步解耦模式：

```go
// 生产者：订单服务
func (s *OrderService) CreateOrder(ctx context.Context, order *Order) error {
    // 保存订单到数据库
    if err := s.repo.Create(ctx, order); err != nil {
        return err
    }

    // 发布订单创建事件
    data, _ := json.Marshal(order)
    return s.mq.Publish(ctx, "orders.created", data)
}

// 消费者：库存服务
func (s *InventoryService) Start(ctx context.Context) error {
    // 队列订阅，实现负载均衡
    sub, err := s.mq.QueueSubscribe(ctx, "orders.created", "inventory_workers", s.handleOrderCreated)
    if err != nil {
        return err
    }
    s.sub = sub
    return nil
}

func (s *InventoryService) handleOrderCreated(ctx context.Context, msg mq.Message) error {
    var order Order
    if err := json.Unmarshal(msg.Data(), &order); err != nil {
        return err
    }

    // 扣减库存
    return s.inventory.Reserve(ctx, order.ProductID, order.Quantity)
}
```

### 2. 广播通知模式

适用于一对多通知场景：

```go
// 配置中心：发布配置更新
func (s *ConfigCenter) PublishConfigUpdate(ctx context.Context, config *Config) error {
    data, _ := json.Marshal(config)
    return s.mq.Publish(ctx, "config.updates", data)
}

// 多个服务：订阅配置更新
func (s *UserService) SubscribeConfigUpdates(ctx context.Context) error {
    sub, err := s.mq.Subscribe(ctx, "config.updates", s.handleConfigUpdate)
    if err != nil {
        return err
    }
    s.configSub = sub
    return nil
}

func (s *UserService) handleConfigUpdate(ctx context.Context, msg mq.Message) error {
    var config Config
    json.Unmarshal(msg.Data(), &config)
    s.updateLocalConfig(config)
    return nil
}
```

        }

        data, _ := json.Marshal(user)
        return s.mq.Publish(ctx, msg.Reply(), data)
    })
    return err

}

````

## 函数式选项

```go
// WithLogger 注入日志记录器
mqClient, err := mq.New(natsConn, cfg, mq.WithLogger(logger))

// WithMeter 注入指标收集器
mqClient, err := mq.New(natsConn, cfg, mq.WithMeter(meter))

// 组合使用
mqClient, err := mq.New(natsConn, cfg,
    mq.WithLogger(logger),
    mq.WithMeter(meter))
````

## 资源所有权模型

MQ 组件采用**借用模型 (Borrowing Model)**：

1. **连接器 (Owner)**：拥有底层连接，负责创建连接池并在应用退出时执行 `Close()`
2. **MQ 组件 (Borrower)**：借用连接器中的客户端，不拥有其生命周期
3. **生命周期控制**：使用 `defer` 确保关闭顺序与创建顺序相反（LIFO）

```go
// ✅ 正确示例
natsConn, _ := connector.NewNATS(&cfg.NATS, connector.WithLogger(logger))
defer natsConn.Close() // 应用结束时关闭底层连接
natsConn.Connect(ctx)

mqClient, _ := mq.New(natsConn, &cfg.MQ, mq.WithLogger(logger))
// mqClient.Close() 为 no-op，但建议调用以保持接口一致性
```

## 与其他组件配合

```go
func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建连接器
    natsConn, _ := connector.NewNATS(&cfg.NATS, connector.WithLogger(logger))
    defer natsConn.Close()
    natsConn.Connect(ctx)

    // 2. 创建 MQ 组件
    mqClient, _ := mq.New(natsConn, &cfg.MQ, mq.WithLogger(logger))

    // 3. 使用 MQ 组件
    orderSvc := service.NewOrderService(mqClient)
    inventorySvc := service.NewInventoryService(mqClient)

    // 启动服务
    go orderSvc.Start(ctx)
    go inventorySvc.Start(ctx)
}
```

## 最佳实践

1. **模式选择**：
    - 实时指标、日志、通知使用 Core 模式
    - 订单处理、状态机流转使用 JetStream 模式
    - 避免混用，保持一致性

2. **主题命名**：
    - 使用 `资源.动作` 格式，如 `orders.created`、`users.updated`
    - 避免过度依赖 NATS 的复杂通配符路由
    - 保持扁平化命名，便于未来迁移到 Kafka

3. **消息处理**：
    - Handler 应该是幂等的，支持重复处理
    - 避免在 Handler 中执行耗时操作
    - 返回 error 会触发重试，谨慎使用

4. **错误处理**：
    - 使用 `xerrors.Wrapf()` 包装错误
    - 区分业务错误和系统错误
    - 业务错误返回 nil，避免不必要的重试

5. **连接管理**：
    - 务必通过 `WithLogger` 和 `WithMeter` 注入可观测性组件
    - 使用 `defer` 确保连接器正确关闭
    - 在应用启动阶段连接，Fail-fast

## 完整示例

```go
package main

import (
    "context"
    "encoding/json"
    "time"

    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/mq"
)

type Order struct {
    ID     string  `json:"id"`
    UserID string  `json:"user_id"`
    Amount float64 `json:"amount"`
}

func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建 NATS 连接器
    natsConn, err := connector.NewNATS(&connector.NATSConfig{
        URL:           "nats://127.0.0.1:4222",
        Name:          "order-service",
        ReconnectWait: 2 * time.Second,
        MaxReconnects: 5,
    }, connector.WithLogger(logger))
    if err != nil {
        panic(err)
    }
    defer natsConn.Close()

    // 2. 连接到 NATS
    if err := natsConn.Connect(ctx); err != nil {
        panic(err)
    }

    // 3. 创建 MQ 客户端（JetStream 模式）
    mqClient, err := mq.New(natsConn, &mq.Config{
        Driver: mq.DriverNatsJetStream,
        JetStream: &mq.JetStreamConfig{
            AutoCreateStream: true,
        },
    }, mq.WithLogger(logger))
    if err != nil {
        panic(err)
    }

    // 4. 启动订单处理器
    sub, err := mqClient.QueueSubscribe(ctx, "orders.created", "order_processors", func(ctx context.Context, msg mq.Message) error {
        var order Order
        if err := json.Unmarshal(msg.Data(), &order); err != nil {
            logger.Error("failed to unmarshal order", clog.Error(err))
            return err
        }

        // 处理订单
        logger.Info("processing order",
            clog.String("order_id", order.ID),
            clog.String("user_id", order.UserID),
            clog.Float64("amount", order.Amount))

        // 模拟处理时间
        time.Sleep(100 * time.Millisecond)

        logger.Info("order processed successfully", clog.String("order_id", order.ID))
        return nil
    })
    if err != nil {
        panic(err)
    }
    defer sub.Unsubscribe()

    // 5. 发布一些测试订单
    for i := 0; i < 5; i++ {
        order := Order{
            ID:     fmt.Sprintf("order-%d", i+1),
            UserID: fmt.Sprintf("user-%d", (i%3)+1),
            Amount: float64((i+1) * 99.99),
        }

        data, _ := json.Marshal(order)
        if err := mqClient.Publish(ctx, "orders.created", data); err != nil {
            logger.Error("failed to publish order", clog.Error(err))
        } else {
            logger.Info("order published", clog.String("order_id", order.ID))
        }
    }

    // 等待处理完成
    time.Sleep(2 * time.Second)
    logger.Info("MQ example completed successfully")
}
```
