# Idempotency 幂等组件示例

本示例演示了 Genesis 框架中幂等组件的使用方法。

## 功能特性

- **原子性保证**: 使用 Redis SETNX 和 Lua 脚本确保并发安全
- **结果缓存**: 支持缓存首次执行的响应结果
- **多场景适配**: 提供 Gin Middleware、直接 API 调用等多种使用方式
- **灵活配置**: 支持自定义 TTL、前缀等配置

## 前置条件

确保 Redis 服务正在运行：

```bash
# 使用 Docker 启动 Redis
docker run -d --name redis -p 6379:6379 redis:latest

# 或使用项目提供的 docker-compose
cd ../../deploy
docker-compose -f redis.yml up -d
```

## 运行示例

```bash
# 构建
go build -o idempotency-example main.go

# 运行
./idempotency-example
```

## 示例说明

### 示例 1: 直接使用幂等组件

演示如何直接调用幂等组件的 API：

```go
// 创建幂等组件
idem, err := idempotency.New(redisConn, &idempotency.Config{
    Prefix:        "example:idem:",
    DefaultTTL:    1 * time.Hour,
    ProcessingTTL: 5 * time.Minute,
}, idempotency.WithLogger(logger))

// 使用幂等控制
result, err := idem.Do(ctx, "order:12345", func() (any, error) {
    // 业务逻辑
    return processOrder()
})
```

**特性演示:**
- 第一次调用：执行业务逻辑并缓存结果
- 第二次调用：直接返回缓存结果，不再执行业务逻辑
- 检查状态：查询幂等键的当前状态
- 删除记录：手动清理幂等记录后可重新执行

### 示例 2: Gin 中间件

演示如何在 Gin Web 框架中使用幂等中间件：

```go
r := gin.New()

// 使用幂等中间件
r.Use(adapter.GinMiddleware(idem, nil, idempotency.WithTTL(30*time.Minute)))

r.POST("/orders", func(c *gin.Context) {
    // 业务逻辑
})
```

**测试命令:**

```bash
# 第一次请求 - 创建订单
curl -X POST http://localhost:8080/orders \
  -H "X-Idempotency-Key: req-001"

# 第二次请求 - 返回缓存结果（注意 counter 值不变）
curl -X POST http://localhost:8080/orders \
  -H "X-Idempotency-Key: req-001"

# 第三次请求 - 使用不同的幂等键创建新订单
curl -X POST http://localhost:8080/orders \
  -H "X-Idempotency-Key: req-002"
```

### 带状态码过滤的中间件

只缓存成功状态（200, 201）的响应：

```go
r.POST("/payments", adapter.GinMiddlewareWithStatus(
    idem,
    nil,
    []int{http.StatusOK, http.StatusCreated}, // 只缓存这些状态码
    idempotency.WithTTL(1*time.Hour),
), func(c *gin.Context) {
    // 支付逻辑
})
```

## 核心 API

### Idempotent 接口

```go
type Idempotent interface {
    // Do 执行幂等操作
    Do(ctx context.Context, key string, fn func() (any, error), opts ...DoOption) (any, error)
    
    // Check 检查幂等键的状态
    Check(ctx context.Context, key string) (Status, any, error)
    
    // Delete 删除幂等记录
    Delete(ctx context.Context, key string) error
}
```

### 配置选项

```go
type Config struct {
    Prefix        string        // Key 前缀，默认 "idempotency:"
    DefaultTTL    time.Duration // 默认记录保留时间，默认 24h
    ProcessingTTL time.Duration // 处理中状态的 TTL，默认 5m
}
```

### Do 方法选项

```go
// 自定义 TTL
idem.Do(ctx, key, fn, idempotency.WithTTL(2*time.Hour))
```

## 状态说明

幂等组件维护三种状态：

- **StatusProcessing**: 处理中（已加锁，业务逻辑正在执行）
- **StatusSuccess**: 处理成功（已缓存结果）
- **StatusFailed**: 处理失败（保留用于扩展）

## 错误处理

```go
result, err := idem.Do(ctx, key, fn)
if err != nil {
    if err == idempotency.ErrProcessing {
        // 请求正在处理中（并发请求）
        // HTTP 返回 429 Too Many Requests
    } else if err == idempotency.ErrKeyEmpty {
        // 幂等键为空
    } else {
        // 其他错误（如 Redis 连接失败）
    }
}
```

## 使用场景

1. **订单创建**: 防止重复下单
2. **支付处理**: 防止重复扣款
3. **消息发送**: 防止重复推送
4. **数据导入**: 防止重复导入

## 最佳实践

1. **合理设置 TTL**: 根据业务特点设置合适的缓存时间
2. **幂等键设计**: 使用唯一标识作为幂等键（如 RequestID、OrderID）
3. **错误处理**: 妥善处理 ErrProcessing 错误，避免误导用户
4. **降级策略**: 当 Redis 不可用时，考虑降级方案

## 注意事项

1. 幂等组件直接依赖 Redis，确保 Redis 可用性
2. ProcessingTTL 应该大于业务逻辑的最大执行时间
3. Gin 中间件会捕获完整的响应内容，注意内存使用
4. 删除操作应谨慎使用，通常让记录自然过期即可
