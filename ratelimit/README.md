# RateLimit 组件

RateLimit 是 Genesis 微服务框架的限流组件，提供**单机**和**分布式**两种模式的限流能力。基于令牌桶算法实现高性能限流，支持多种使用场景。

## 特性

- ✅ **令牌桶算法** - 经典的限流算法，支持突发流量
- ✅ **单机模式** - 基于内存的快速限流，零外部依赖
- ✅ **分布式模式** - 基于 Redis 的分布式限流，支持多进程/多服务器
- ✅ **灵活配置** - 支持按 IP、用户 ID、服务名等维度限流
- ✅ **Gin 中间件** - 开箱即用的 Gin 集成
- ✅ **指标埋点** - 完整的可观测性支持（OpenTelemetry）
- ✅ **错误处理** - 统一的错误定义与处理

## 目录结构（完全扁平化设计）

```
ratelimit/
├── ratelimit.go          # 核心接口、配置与工厂函数
├── standalone.go         # 单机限流器实现
├── distributed.go        # 分布式限流器实现
├── options.go            # 初始化选项函数
├── errors.go             # 错误定义（使用 xerrors）
├── metrics.go            # 指标常量定义
├── middleware.go         # Gin 中间件
└── README.md             # 本文件
```

**设计原则**：完全扁平化设计，所有公开 API 和实现都在根目录，无 `types/` 子包

## 快速开始

### 单机模式

```go
import (
    "context"
    "github.com/ceyewan/genesis/ratelimit"
    "github.com/ceyewan/genesis/clog"
)

// 创建 Logger
logger, _ := clog.New(&clog.Config{Level: "info"})

// 创建单机限流器
limiter, _ := ratelimit.New(&ratelimit.Config{
    Mode: ratelimit.ModeStandalone,
    Standalone: ratelimit.StandaloneConfig{
        CleanupInterval: 1 * time.Minute,
        IdleTimeout:     5 * time.Minute,
    },
}, nil, ratelimit.WithLogger(logger))

// 定义限流规则：10 QPS，突发 20
limit := ratelimit.Limit{Rate: 10, Burst: 20}

// 检查是否允许请求
allowed, err := limiter.Allow(context.Background(), "user:123", limit)
if err != nil {
    // 处理系统错误
}

if !allowed {
    // 请求被限流
    return "rate limit exceeded"
}

// 请求通过，继续处理业务逻辑
```

### 分布式模式

```go
import (
    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/ratelimit"
)

// 创建 Redis 连接器
redisConn, _ := connector.NewRedis(&connector.RedisConfig{
    Addr: "127.0.0.1:6379",
}, connector.WithLogger(logger))
defer redisConn.Close()

// 创建分布式限流器
limiter, _ := ratelimit.New(&ratelimit.Config{
    Mode: ratelimit.ModeDistributed,
    Distributed: ratelimit.DistributedConfig{
        Prefix: "api:ratelimit:",
    },
}, redisConn, ratelimit.WithLogger(logger))

// 使用方式与单机模式相同
allowed, _ := limiter.Allow(ctx, "user:123", limit)
```

## 核心接口

### Limiter 接口

```go
type Limiter interface {
    // Allow 尝试获取 1 个令牌 (非阻塞)
    Allow(ctx context.Context, key string, limit Limit) (bool, error)

    // AllowN 尝试获取 N 个令牌 (非阻塞)
    AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error)

    // Wait 阻塞等待直到获取 1 个令牌
    Wait(ctx context.Context, key string, limit Limit) error
}
```

### Limit 配置

```go
type Limit struct {
    Rate  float64 // 令牌生成速率 (每秒)
    Burst int     // 令牌桶容量 (最大突发请求数)
}

// 示例
limit := Limit{Rate: 100, Burst: 200}
// 表示：每秒生成 100 个令牌，桶容量 200
// 可在短时间内处理最多 200 个请求
// 平均每秒可处理 100 个请求
```

## 配置结构

### Config

```go
type Config struct {
    Mode        Mode                // 限流模式：standalone | distributed
    Standalone  StandaloneConfig    // 单机模式配置
    Distributed DistributedConfig   // 分布式模式配置
}
```

### StandaloneConfig

```go
type StandaloneConfig struct {
    CleanupInterval time.Duration // 清理过期限流器的间隔 (默认: 1m)
    IdleTimeout     time.Duration // 限流器空闲超时 (默认: 5m)
}
```

### DistributedConfig

```go
type DistributedConfig struct {
    Prefix string // Redis Key 前缀 (默认: "ratelimit:")
}
```

## 应用场景

### 1. API 服务限流

```go
// 为 HTTP API 设置全局限流
r := gin.New()
r.Use(ratelimit.GinMiddleware(limiter, nil, func(c *gin.Context) ratelimit.Limit {
    return ratelimit.Limit{Rate: 1000, Burst: 2000}
}))
```

### 2. 用户级别限流

```go
// 为每个用户设置独立的限流规则
r.Use(ratelimit.GinMiddlewarePerUser(limiter, func(c *gin.Context) ratelimit.Limit {
    // 不同用户的限流规则可能不同
    return ratelimit.Limit{Rate: 100, Burst: 200}
}))
```

### 3. 路径级别限流

```go
// 为不同路径设置不同的限流规则
pathLimits := map[string]ratelimit.Limit{
    "/api/login":   {Rate: 5, Burst: 10},    // 登录接口限流严格
    "/api/data":    {Rate: 100, Burst: 200}, // 数据接口限流宽松
    "/api/upload":  {Rate: 2, Burst: 5},     // 上传接口限流最严格
}

r.Use(ratelimit.GinMiddlewarePerPath(limiter, pathLimits,
    ratelimit.Limit{Rate: 50, Burst: 100}))
```

### 4. 自定义限流键

```go
// 基于自定义业务逻辑的限流
r.Use(ratelimit.GinMiddleware(limiter,
    func(c *gin.Context) string {
        // 可以组合多个维度
        return fmt.Sprintf("user:%s:api:%s",
            c.GetString("user_id"),
            c.Request.URL.Path)
    },
    func(c *gin.Context) ratelimit.Limit {
        return ratelimit.Limit{Rate: 100, Burst: 200}
    }))
```

## 可观测性

### 指标 (Metrics)

RateLimit 组件定义了以下指标常量：

```go
// 记录限流检查总数
ratelimit.MetricAllowTotal = "ratelimit_allow_total"

// 记录允许通过的请求数
ratelimit.MetricAllowed = "ratelimit_allowed_total"

// 记录被拒绝的请求数
ratelimit.MetricDenied = "ratelimit_denied_total"

// 记录错误数
ratelimit.MetricErrors = "ratelimit_errors_total"

// 标签
ratelimit.LabelMode      // "mode" - standalone/distributed
ratelimit.LabelKey       // "key" - 限流键
ratelimit.LabelErrorType // "error_type" - 错误类型
```

### 日志

```go
// 传入 Logger 进行日志记录
limiter, err := ratelimit.New(cfg, redisConn,
    ratelimit.WithLogger(logger))
```

## 错误处理

```go
import "github.com/ceyewan/genesis/ratelimit"

// 定义的错误
ratelimit.ErrConfigNil       // 配置为空
ratelimit.ErrConnectorNil    // 连接器为空（分布式模式）
ratelimit.ErrNotSupported    // 操作不支持
ratelimit.ErrKeyEmpty        // 限流键为空
ratelimit.ErrInvalidLimit    // 限流规则无效

// 示例
allowed, err := limiter.Allow(ctx, key, limit)
if err != nil {
    logger.Error("rate limit check failed", clog.Error(err))
    // 根据错误类型决定是否降级放行
}
```

## 工厂函数

### New

```go
func New(cfg *Config, redisConn connector.RedisConnector, opts ...Option) (Limiter, error)
```

创建限流组件实例（独立模式），这是标准的工厂函数，支持在不依赖容器的情况下独立实例化。

**参数**：

- `cfg`: 限流组件配置
- `redisConn`: Redis 连接器（仅分布式模式需要，单机模式传 nil）
- `opts`: 可选参数（Logger, Meter）

**根据配置模式自动选择单机或分布式实现**。

**使用示例**：

```go
// 单机模式
limiter, _ := ratelimit.New(&ratelimit.Config{
    Mode: ratelimit.ModeStandalone,
}, nil, ratelimit.WithLogger(logger))

// 分布式模式
limiter, _ := ratelimit.New(&ratelimit.Config{
    Mode: ratelimit.ModeDistributed,
}, redisConn, ratelimit.WithLogger(logger))
```

### 选项函数

```go
func WithLogger(logger clog.Logger) Option
func WithMeter(meter metrics.Meter) Option
```

## Gin 中间件

### GinMiddleware - 基础中间件

```go
func GinMiddleware(
    limiter Limiter,
    keyFunc func(*gin.Context) string,      // 提取限流键，nil 时使用客户端 IP
    limitFunc func(*gin.Context) Limit,     // 获取限流规则
) gin.HandlerFunc
```

### GinMiddlewareWithHeaders - 带响应头的中间件

```go
func GinMiddlewareWithHeaders(
    limiter Limiter,
    keyFunc func(*gin.Context) string,
    limitFunc func(*gin.Context) Limit,
) gin.HandlerFunc

// 返回的响应头
// X-RateLimit-Limit: rate=100.00, burst=200
// X-RateLimit-Remaining: 0 (被限流时)
```

### GinMiddlewarePerUser - 用户级限流

```go
func GinMiddlewarePerUser(
    limiter Limiter,
    limitFunc func(*gin.Context) Limit,
) gin.HandlerFunc

// 从 context 中读取 "userID" 字段进行限流
```

### GinMiddlewarePerPath - 路径级限流

```go
func GinMiddlewarePerPath(
    limiter Limiter,
    pathLimits map[string]Limit,        // 路径 -> 限流规则
    defaultLimit Limit,                  // 默认限流规则
) gin.HandlerFunc
```

## 最佳实践

### 1. 合理设置限流参数

```go
// ❌ 太严格，影响用户体验
limit := ratelimit.Limit{Rate: 1, Burst: 1}

// ✅ 合理的限流规则
// 普通 API：100 QPS，允许 200 突发
limit := ratelimit.Limit{Rate: 100, Burst: 200}

// 登录接口：5 QPS，允许 10 突发（防止暴力破解）
limit := ratelimit.Limit{Rate: 5, Burst: 10}
```

### 2. 分级限流

```go
// 不同的用户级别有不同的限流规则
func getUserLimit(userID string) ratelimit.Limit {
    // 查询用户等级，返回对应的限流规则
    if isPremium(userID) {
        return ratelimit.Limit{Rate: 10000, Burst: 20000}
    }
    return ratelimit.Limit{Rate: 1000, Burst: 2000}
}
```

### 3. 错误处理降级

```go
allowed, err := limiter.Allow(ctx, key, limit)
if err != nil {
    // 限流器出错时，考虑降级放行或拒绝
    logger.Error("rate limit error", clog.Error(err))
    // 可选：记录指标，告警
}

if !allowed {
    c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
        "error": "rate limit exceeded",
    })
}
```

### 4. 分布式部署注意事项

```go
// 确保所有实例使用相同的 Redis 实例和 Prefix
limiter, _ := ratelimit.New(&ratelimit.Config{
    Mode: ratelimit.ModeDistributed,
    Distributed: ratelimit.DistributedConfig{
        Prefix: "myservice:ratelimit:", // 命名需要避免冲突
    },
}, redisConn)
```

## 完整示例

参见 [examples/ratelimit/main.go](../examples/ratelimit/main.go)

```bash
# 运行示例
make example-ratelimit
```

## 对比表：单机 vs 分布式

| 特性             | 单机               | 分布式               |
| ---------------- | ------------------ | -------------------- |
| **延迟**         | 极低（微秒级）     | 低（毫秒级）         |
| **内存占用**     | 低                 | 取决于 Redis         |
| **支持多进程**   | ❌ 否              | ✅ 是                |
| **支持多服务器** | ❌ 否              | ✅ 是                |
| **依赖外部服务** | ❌ 否              | ✅ Redis             |
| **适用场景**     | 单机应用、本地限流 | 分布式系统、集群环境 |

## 参考文献

- [令牌桶算法](https://en.wikipedia.org/wiki/Token_bucket)
- [Genesis 架构设计](../docs/genesis-design.md)
