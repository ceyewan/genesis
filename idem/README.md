# Idempotency - 幂等性组件

`idem` 是 Genesis 框架中的幂等性组件，用于确保在分布式环境中操作的"一次且仅一次"执行。适用于 MQ 消费、HTTP 请求（Gin）、RPC 调用（gRPC）等多种场景。

## 特性

- **多场景支持**：支持手动调用、Gin 中间件、gRPC 拦截器等多种使用方式
- **结果缓存**：自动缓存执行结果，重复请求直接返回缓存数据
- **并发控制**：内置分布式锁机制，防止同一幂等键的并发穿透
- **Redis 后端**：基于 Redis 存储结果与锁
- **显式依赖注入**：通过 `connector` 注入 Redis 连接

## 目录结构

idem 采用完全扁平化设计，所有文件直接位于包目录下：

```
idem/
├── README.md           # 本文件：组件文档
├── idem.go      # 核心接口: Idempotency, New()
├── config.go          # 配置定义
├── store.go            # 存储接口: Store
├── redis.go            # Redis 存储实现
├── options.go          # 组件选项: Option, WithLogger(), WithRedisConnector()
├── middleware.go       # Gin 中间件
├── interceptor.go      # gRPC 拦截器
└── errors.go           # 错误定义
```

## 快速开始

### 基础使用（手动模式）

适用于 MQ 消费端或业务逻辑内部调用。

```go
package main

import (
    "context"
    "time"

    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/config"
    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/idem"
)

func main() {
    // 1. 初始化依赖
    cfg, _ := config.Load("config.yaml")
    logger, _ := clog.New(&cfg.Log)
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()

    // 2. 创建幂等组件
    idem, _ := idem.New(&idem.Config{
        Driver:     idem.DriverRedis,
        Prefix:     "myapp:idem:",
        DefaultTTL: 24 * time.Hour, // 幂等记录保留 24 小时
    }, idem.WithRedisConnector(redisConn), idem.WithLogger(logger))

    // 3. 执行幂等操作
    ctx := context.Background()
    key := "order:create:12345" // 唯一的幂等键

    result, err := idem.Execute(ctx, key, func(ctx context.Context) (interface{}, error) {
        // 业务逻辑：创建订单
        logger.Info("creating order...")
        return map[string]interface{}{"order_id": "12345", "status": "created"}, nil
    })

    if err != nil {
        // 处理错误 (如并发冲突、存储失败等)
        logger.Error("operation failed", clog.Error(err))
        return
    }

    logger.Info("operation result", clog.Any("result", result))
}
```

### Gin 中间件

适用于 HTTP API 服务，自动处理 `X-Idempotency-Key` 头。

```go
import (
    "github.com/gin-gonic/gin"
    "github.com/ceyewan/genesis/idem"
)

func main() {
    // ... 初始化 idem ...

    r := gin.Default()

    // 使用中间件
    // 默认从 Header "X-Idempotency-Key" 获取键
    // 自动缓存响应状态码和 Body
    r.POST("/orders", idem.GinMiddleware(), func(c *gin.Context) {
        // 业务逻辑
        c.JSON(200, gin.H{"order_id": "123"})
    })

    r.Run(":8080")
}
```

### gRPC 拦截器

适用于 gRPC 服务端，防止重复 RPC 调用。

```go
import (
    "google.golang.org/grpc"
    "github.com/ceyewan/genesis/idem"
)

func main() {
    // ... 初始化 idem ...

    // 注册拦截器
    // 客户端需在 Metadata 中传递幂等键
    s := grpc.NewServer(
        grpc.UnaryInterceptor(idem.UnaryServerInterceptor()),
    )

    // ...
}
```

## 核心接口

### Idempotency 接口

```go
type Idempotency interface {
    // Execute 执行幂等操作
    // 如果 key 已存在且完成，直接返回缓存结果
    // 如果 key 正在处理中，返回 ErrConcurrentRequest
    // 如果 key 不存在，执行 fn 并缓存结果
    Execute(ctx context.Context, key string, fn func(ctx context.Context) (interface{}, error)) (interface{}, error)

    // GinMiddleware 返回 Gin 中间件
    GinMiddleware(opts ...MiddlewareOption) interface{}

    // UnaryServerInterceptor 返回 gRPC Unary 拦截器
    UnaryServerInterceptor(opts ...InterceptorOption) grpc.UnaryServerInterceptor
}
```

### Store 接口

```go
type Store interface {
    // Lock 尝试获取锁（标记处理中）
    Lock(ctx context.Context, key string, ttl time.Duration) (bool, error)

    // Unlock 释放锁（通常用于执行失败时清理）
    Unlock(ctx context.Context, key string) error

    // SetResult 保存执行结果并标记完成
    SetResult(ctx context.Context, key string, val []byte, ttl time.Duration) error

    // GetResult 获取已完成的结果
    GetResult(ctx context.Context, key string) ([]byte, error)
}
```

## 配置说明

```go
type Config struct {
    // Driver 后端类型: "redis" (默认 "redis")
    Driver DriverType

    // Prefix Redis Key 前缀，默认 "idem:"
    Prefix string

    // DefaultTTL 幂等记录有效期，默认 24h
    DefaultTTL time.Duration

    // LockTTL 处理过程中的锁超时时间，默认 30s
    // 防止业务逻辑崩溃导致死锁
    LockTTL time.Duration
}
```

## 错误处理

组件使用 `xerrors` 定义错误：

- `ErrConcurrentRequest`: 并发请求（锁被占用）
- `ErrResultNotFound`: 结果未找到（内部使用）

## 最佳实践

1. **Key 的选择**：确保 Key 具有全局唯一性且与业务操作一一对应。例如 `source_id + event_id` 或 `user_id + request_id`。
2. **TTL 设置**：根据业务对"重复"的定义设置 TTL。例如订单支付可能只需要 1 小时的幂等窗口，而某些财务操作可能需要更久。
3. **错误处理**：`Execute` 中的 `fn` 如果返回 error，幂等组件通常**不会**缓存结果（除非配置了缓存错误），以便允许重试。
4. **Gin/gRPC 响应**：中间件模式下，组件会缓存 HTTP/gRPC 的响应体。请注意响应体的大小，避免 Redis 存储压力过大。
