# RateLimit 限流组件设计文档

## 1. 目标与原则

`ratelimit` 旨在为微服务框架提供一个统一、灵活且高性能的限流组件。它支持单机和分布式两种模式，并提供开箱即用的中间件适配器。

**设计原则：**

1. **接口抽象 (Abstraction):** 业务代码只依赖 `ratelimit.Limiter` 接口，不感知底层实现（单机或分布式）。
2. **模式灵活 (Flexible Modes):** 支持单机模式（基于内存）和分布式模式（基于 Redis），可通过配置切换。
3. **动态策略 (Dynamic Policy):** 支持在运行时动态指定限流规则（Rate/Burst），适应不同用户等级或业务场景。
4. **无侵入集成 (Non-intrusive):** 提供 Gin Middleware 和 gRPC Interceptor，业务逻辑无需修改即可接入限流。
5. **自我维护 (Self-maintenance):** 单机模式具备自动清理机制，防止内存泄漏；分布式模式利用 Redis 过期机制。

## 2. 项目结构

遵循框架整体的分层设计，API 与实现分离：

```text
genesis/
├── pkg/
│   └── ratelimit/              # 公开 API 入口
│       ├── ratelimit.go        # 工厂函数 (New)
│       ├── adapter/            # 适配器
│       │   ├── gin.go          # Gin Middleware
│       │   └── grpc.go         # gRPC Interceptor
│       └── types/              # 类型定义
│           ├── interface.go    # Limiter 接口
│           └── config.go       # 配置定义
├── internal/
│   └── ratelimit/              # 内部实现
│       ├── standalone/         # 单机实现 (x/time/rate)
│       │   └── limiter.go
│       └── distributed/        # 分布式实现 (Redis Lua)
│           └── limiter.go
└── ...
```

## 3. 核心 API 设计

核心定义位于 `pkg/ratelimit/types/`。

### 3.1. Limiter 接口

```go
// pkg/ratelimit/types/interface.go

package types

import (
    "context"
)

// Limit 定义限流规则 (令牌桶算法)
type Limit struct {
    Rate  float64 // 令牌生成速率 (每秒生成多少个令牌)
    Burst int     // 令牌桶容量 (突发最大请求数)
}

// Limiter 限流器核心接口
type Limiter interface {
    // Allow 尝试获取 1 个令牌 (非阻塞)
    // key: 限流标识 (如 IP, UserID, ServiceName)
    // limit: 限流规则
    // 返回: allowed (是否允许), error (系统错误)
    Allow(ctx context.Context, key string, limit Limit) (bool, error)

    // AllowN 尝试获取 N 个令牌 (非阻塞)
    AllowN(ctx context.Context, key string, limit Limit, n int) (bool, error)

    // Wait 阻塞等待直到获取 1 个令牌
    // 注意：分布式实现可能不支持此方法或性能较低
    Wait(ctx context.Context, key string, limit Limit) error
}
```

### 3.2. 配置 (Config)

```go
// pkg/ratelimit/types/config.go

package types

import "time"

type Mode string

const (
 ModeStandalone  Mode = "standalone"
 ModeDistributed Mode = "distributed"
)

type Config struct {
 Mode Mode `yaml:"mode" json:"mode"` // standalone | distributed

 // RedisConnector 引用 connector 的名称 (仅 Distributed 模式需要)
 RedisConnector string `yaml:"redis_connector" json:"redis_connector"`
 
 // 单机模式配置
 Standalone struct {
  // CleanupInterval 清理过期限流器的间隔
  // 默认: 1m
  CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
 } `yaml:"standalone" json:"standalone"`
}
```

## 4. 内部实现设计

### 4.1. 单机限流 (Standalone)

* **底层依赖**: `golang.org/x/time/rate`
* **管理机制**: 使用 `sync.Map` 或带锁的 `map[string]*wrapper` 管理不同 Key 的限流器。
* **内存回收**:
  * 包装 `rate.Limiter`，增加 `lastSeen` 字段。
  * 启动后台 Goroutine，每隔 `CleanupInterval` 扫描 Map，删除长时间未使用的 Limiter。

### 4.2. 分布式限流 (Distributed)

* **底层依赖**: Redis + Lua Script。
* **算法**: 令牌桶 (Token Bucket)。
* **Lua 逻辑**:
    1. 获取当前 tokens 和 上次刷新时间。
    2. 计算新生成的 tokens: `delta = (now - last_ts) * rate`。
    3. 更新 tokens: `tokens = min(burst, tokens + delta)`。
    4. 判断是否足够: `if tokens >= requested { tokens -= requested; return 1 } else { return 0 }`。
    5. 保存状态: `tokens` 和 `now`。
* **Wait 支持**: 分布式环境下 `Wait` 难以精确实现且代价高昂，初期版本返回 `ErrNotSupported` 或建议客户端轮询。

## 5. 适配器设计 (Adapters)

### 5.1. Gin Middleware

```go
// pkg/ratelimit/adapter/gin.go

func RateLimitMiddleware(l types.Limiter, keyFunc func(*gin.Context) string, limitFunc func(*gin.Context) types.Limit) gin.HandlerFunc {
    return func(c *gin.Context) {
        key := keyFunc(c)
        limit := limitFunc(c)
        
        allowed, err := l.Allow(c, key, limit)
        if err != nil {
            // 降级策略：报错时默认放行或拒绝，视业务重要性而定
            // 这里建议记录日志并放行，避免限流组件故障影响业务
            c.Next() 
            return
        }
        
        if !allowed {
            c.AbortWithStatus(http.StatusTooManyRequests)
            return
        }
        c.Next()
    }
}
```

### 5.2. gRPC Interceptor

```go
// pkg/ratelimit/adapter/grpc.go

func UnaryServerInterceptor(l types.Limiter, keyFunc func(context.Context, any) string, limitFunc func(...) types.Limit) grpc.UnaryServerInterceptor {
    // ... 实现类似 Gin Middleware 的逻辑
    // 拒绝时返回 status.Errorf(codes.ResourceExhausted, "rate limit exceeded")
}
```

## 6. 使用示例

```go
func main() {
    // 1. 初始化
    cfg := &types.Config{Mode: types.ModeStandalone}
    limiter, _ := ratelimit.New(cfg, nil) // nil for redis connector in standalone

    // 2. 定义规则
    limit := types.Limit{Rate: 10, Burst: 20} // 10 QPS

    // 3. 手动调用
    if allowed, _ := limiter.Allow(ctx, "user:123", limit); !allowed {
        fmt.Println("Limited!")
    }

    // 4. Gin 集成
    r := gin.New()
    r.Use(adapter.RateLimitMiddleware(limiter, 
        func(c *gin.Context) string { return c.ClientIP() },
        func(c *gin.Context) types.Limit { return limit },
    ))
}
