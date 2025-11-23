# RateLimit 限流组件示例

本示例演示了 Genesis 框架中限流组件的使用方法。

## 功能特性

- **双模式支持**: 单机模式（内存）和分布式模式（Redis）
- **令牌桶算法**: 基于时间戳的高效令牌桶实现
- **灵活配置**: 支持动态限流规则（Rate + Burst）
- **多场景适配**: 提供 Gin Middleware、直接 API 调用等多种使用方式
- **自动清理**: 单机模式自动清理过期限流器，防止内存泄漏

## 前置条件

### 单机模式

无需外部依赖，开箱即用。

### 分布式模式

需要 Redis 服务：

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
go build -o ratelimit-example main.go

# 运行
./ratelimit-example
```

## 示例说明

### 示例 1: 单机模式

演示基于内存的限流：

```go
limiter, _ := ratelimit.New(&types.Config{
    Mode: types.ModeStandalone,
    Standalone: types.StandaloneConfig{
        CleanupInterval: 1 * time.Minute,
        IdleTimeout:     5 * time.Minute,
    },
}, nil, ratelimit.WithLogger(logger))

// 定义限流规则
limit := types.Limit{
    Rate:  5,   // 每秒 5 个请求
    Burst: 10,  // 突发允许 10 个
}

// 检查是否允许
allowed, _ := limiter.Allow(ctx, "user:123", limit)
```

**特性演示:**

- 快速发送 20 个请求，观察限流效果
- 等待 1 秒后重试，观察令牌恢复

### 示例 2: 分布式模式

演示基于 Redis 的分布式限流：

```go
limiter, _ := ratelimit.New(&types.Config{
    Mode: types.ModeDistributed,
    Distributed: types.DistributedConfig{
        Prefix: "example:ratelimit:",
    },
}, redisConn, ratelimit.WithLogger(logger))

// 使用 Lua 脚本实现原子性限流检查
allowed, _ := limiter.Allow(ctx, "api:/users", limit)
```

**优势:**

- 多实例共享限流状态
- 利用 Redis 的高性能
- 基于 Lua 脚本保证原子性

### 示例 3: Gin 中间件

演示在 Web 框架中使用限流：

```go
r := gin.New()

// 全局限流
r.Use(adapter.GinMiddleware(limiter, nil, func(c *gin.Context) types.Limit {
    return types.Limit{Rate: 10, Burst: 20}
}))

// 路径级限流
pathLimits := map[string]types.Limit{
    "/api/login":  {Rate: 5, Burst: 10},    // 严格限流
    "/api/data":   {Rate: 100, Burst: 200}, // 宽松限流
    "/api/upload": {Rate: 2, Burst: 5},     // 最严格
}
r.Use(adapter.GinMiddlewarePerPath(limiter, pathLimits, defaultLimit))
```

**测试命令:**

```bash
# 测试全局限流 (10 QPS)
for i in {1..15}; do curl http://localhost:8080/api/test; echo; done

# 测试登录接口限流 (5 QPS)
for i in {1..10}; do curl -X POST http://localhost:8080/api/login; echo; done

# 测试上传接口限流 (2 QPS)
for i in {1..5}; do curl -X POST http://localhost:8080/api/upload; echo; done
```

## 核心 API

### Limiter 接口

```go
type Limiter interface {
    // Allow 尝试获取 1 个令牌（非阻塞）
    Allow(ctx context.Context, key string, limit Limit) (bool, error)
    
    // AllowN 尝试获取 N 个令牌（非阻塞）
    AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error)
    
    // Wait 阻塞等待直到获取 1 个令牌
    // 注意：分布式模式不支持此方法
    Wait(ctx context.Context, key string, limit Limit) error
}
```

### 限流规则

```go
type Limit struct {
    Rate  float64 // 令牌生成速率（每秒生成多少个令牌）
    Burst int     // 令牌桶容量（突发最大请求数）
}
```

### 配置选项

```go
// 单机模式
&Config{
    Mode: ModeStandalone,
    Standalone: StandaloneConfig{
        CleanupInterval: 1 * time.Minute,  // 清理间隔
        IdleTimeout:     5 * time.Minute,  // 空闲超时
    },
}

// 分布式模式
&Config{
    Mode: ModeDistributed,
    Distributed: DistributedConfig{
        Prefix: "ratelimit:",  // Redis Key 前缀
    },
}
```

## 中间件适配器

### 基础中间件

```go
adapter.GinMiddleware(limiter, keyFunc, limitFunc)
```

### 带响应头的中间件

```go
adapter.GinMiddlewareWithHeaders(limiter, keyFunc, limitFunc)
// 会添加 X-RateLimit-Limit 和 X-RateLimit-Remaining 响应头
```

### 基于用户的限流

```go
adapter.GinMiddlewarePerUser(limiter, limitFunc)
// 从 context 获取 userID 作为限流键
```

### 基于路径的限流

```go
adapter.GinMiddlewarePerPath(limiter, pathLimits, defaultLimit)
// 不同路径使用不同的限流规则
```

## 令牌桶算法说明

本组件使用**基于时间戳的令牌桶算法**：

1. **时间戳方式**: 只存储一个"下次允许请求的时间戳"
2. **原子性**: 分布式模式使用 Lua 脚本保证原子性
3. **高效**: 避免存储令牌数量，计算更简单
4. **精确**: 支持浮点数速率，精度更高

### 算法核心逻辑

```
下次可用时间 = max(上次记录时间, 当前时间)
新的记录时间 = 下次可用时间 + (请求令牌数 / 速率)
最远允许时间 = 当前时间 + (桶容量 / 速率)

if 新的记录时间 <= 最远允许时间:
    允许请求
    更新记录时间
else:
    拒绝请求
```

## 使用场景

1. **API 限流**: 防止 API 被恶意调用
2. **用户限流**: 不同用户使用不同的限流规则
3. **路径限流**: 不同接口使用不同的限流策略
4. **登录保护**: 防止暴力破解
5. **资源保护**: 保护数据库、外部 API 等资源

## 最佳实践

1. **合理设置 Rate 和 Burst**:
   - Rate: 平均每秒允许的请求数
   - Burst: 突发允许的最大请求数（通常设为 Rate 的 2-3 倍）

2. **选择合适的模式**:
   - 单实例应用：使用单机模式（性能更好）
   - 多实例应用：使用分布式模式（状态共享）

3. **限流键设计**:
   - 基于 IP: `c.ClientIP()`
   - 基于用户: `"user:" + userID`
   - 基于API: `"api:" + path`
   - 组合键: `IP + ":" + path`

4. **错误处理**:
   - 限流器故障时考虑降级策略
   - 记录限流日志便于分析

5. **返回友好提示**:
   - HTTP 429 Too Many Requests
   - 返回 Retry-After 头告知客户端等待时间

## 性能对比

| 模式 | QPS | 延迟 | 多实例支持 | 内存使用 |
|------|-----|------|-----------|---------|
| 单机 | ~100K | <1μs | ✗ | 低 |
| 分布式 | ~10K | ~1ms | ✓ | 极低（Redis） |

## 注意事项

1. 单机模式的限流器会在空闲超时后自动清理
2. 分布式模式依赖 Redis，需确保 Redis 可用性
3. Wait 方法在分布式模式下不支持（返回 ErrNotSupported）
4. 限流粒度最小为毫秒级（浮点数时间戳）
5. Burst 值应该大于等于并发请求数，避免正常请求被限流
