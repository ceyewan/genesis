# idem - 幂等性组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/idem.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/idem)

分布式幂等性组件，确保操作的"一次且仅一次"执行。支持手动调用、Gin 中间件、gRPC 拦截器。

## 特性

- **多场景支持**：手动调用、Gin 中间件、gRPC 拦截器
- **结果缓存**：自动缓存执行结果，重复请求直接返回
- **并发控制**：内置分布式锁，防止并发穿透
- **双驱动**：Redis / Memory（Memory 仅单机）

## 快速开始

### 手动模式

适用于 MQ 消费、RPC 调用等需要显式控制的场景。

```go
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()

idem, _ := idem.New(&idem.Config{
    Driver:     idem.DriverRedis,
    Prefix:     "myapp:idem:",
    DefaultTTL: 24 * time.Hour,
}, idem.WithRedisConnector(redisConn), idem.WithLogger(logger))

result, err := idem.Execute(ctx, "order:create:12345", func(ctx context.Context) (interface{}, error) {
    // 只在第一次请求时执行
    return createOrder(ctx, req)
})
```

### Gin 中间件

自动从 `X-Idempotency-Key` 头提取幂等键，缓存 HTTP 响应。

```go
r := gin.Default()
r.POST("/orders",
    gin.HandlerFunc(idem.GinMiddleware().(func(*gin.Context))),
    handler,
)
```

### 消息消费去重

仅需判断是否消费过，不缓存结果。

```go
executed, err := idem.Consume(ctx, "msg:"+msgID, 30*time.Minute, func(ctx context.Context) error {
    return handleMessage(ctx, msg)
})
if !executed {
    return // 已消费过
}
```

### gRPC 拦截器

客户端在 metadata 中传递幂等键，服务端自动处理。

```go
s := grpc.NewServer(
    grpc.UnaryInterceptor(idem.UnaryServerInterceptor()),
)
```

## 核心 API

### Idempotency

```go
type Idempotency interface {
    // Execute 执行幂等操作，返回结果或缓存
    Execute(ctx, key string, fn func(ctx) (interface{}, error)) (interface{}, error)

    // Consume 消息消费去重，返回是否执行了 fn
    Consume(ctx, key string, ttl time.Duration, fn func(ctx) error) (bool, error)

    // GinMiddleware 返回 Gin 中间件
    GinMiddleware(opts ...MiddlewareOption) interface{}

    // UnaryServerInterceptor 返回 gRPC 拦截器
    UnaryServerInterceptor(opts ...InterceptorOption) grpc.UnaryServerInterceptor
}
```

### Config

```go
type Config struct {
    Driver      DriverType   // redis | memory
    Prefix      string       // Key 前缀，默认 "idem:"
    DefaultTTL  time.Duration // 结果有效期，默认 24h
    LockTTL     time.Duration // 锁超时，默认 30s
    WaitTimeout time.Duration // 等待结果超时，默认 0
    WaitInterval time.Duration // 轮询间隔，默认 50ms
}
```

## 工作原理

| 状态 | Redis Key | 说明 |
|------|-----------|------|
| 锁定中 | `{prefix}{key}:lock` | 正在处理，其他请求等待 |
| 已完成 | `{prefix}{key}:result` | 处理完成，返回缓存结果 |

锁使用随机 token 保证安全性，避免误删。

## 中间件选项

```go
// 自定义 HTTP 头名称
idem.GinMiddleware(idem.WithHeaderKey("X-Request-ID"))

// 自定义 gRPC metadata 键
idem.UnaryServerInterceptor(idem.WithMetadataKey("idem-key"))
```

## 标准错误

```go
var (
    ErrConfigNil        = xerrors.New("idem: config is nil")
    ErrKeyEmpty         = xerrors.New("idem: key is empty")
    ErrConcurrentRequest = xerrors.New("idem: concurrent request detected")
    ErrResultNotFound   = xerrors.New("idem: result not found")
)
```

## 最佳实践

1. **Key 设计**：确保全局唯一，如 `source_id:event_id` 或 `user_id:request_id`
2. **TTL 设置**：根据业务"重复窗口"设置，订单支付 1h，财务操作可能更长
3. **错误不缓存**：`fn` 返回 error 时不会缓存结果，允许重试
4. **响应大小**：Gin/gRPC 会缓存完整响应，注意 Redis 存储压力
5. **4xx 响应**：HTTP 中间件仅缓存 2xx 响应，4xx 不缓存（客户端参数错误）

## 测试

```bash
go test -v ./idem
```

## 示例

参考 [examples/idem](../examples/idem)。
