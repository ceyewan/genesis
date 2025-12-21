# cache - Genesis 缓存组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/cache.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/cache)

`cache` 是 Genesis 业务层的核心组件，提供基于 Redis 的缓存操作能力，支持多种 Redis 数据结构。

## 特性

- **所属层级**：L2 (Business) — 业务能力，提供缓存抽象
- **核心职责**：在 Redis 连接器的基础上提供统一的缓存操作语义
- **设计原则**：
    - **借用模型**：借用 Redis 连接器的连接，不负责连接的生命周期
    - **原生体验**：保持 Redis 的原汁原味，提供简洁直观的操作接口
    - **自动序列化**：自动处理对象的序列化和反序列化，业务层直接操作结构体
    - **统一命名空间**：提供透明的 Key 前缀管理，确保不同应用的隔离性
    - **完整覆盖**：支持 Redis 的核心数据结构（String、Hash、Sorted Set、List）
    - **可观测性**：集成 clog 和 metrics，提供完整的日志和指标能力

## 目录结构（完全扁平化设计）

```text
cache/                     # 公开 API + 实现（完全扁平化）
├── README.md              # 本文档
├── cache.go               # Cache 接口和实现，New 构造函数
├── config.go              # 配置结构：Config + SetDefaults/Validate
├── options.go             # 函数式选项：Option、WithLogger/WithMeter
├── redis.go               # Redis 具体实现
├── serializer/            # 序列化器子包
│   └── serializer.go      # Serializer 接口和 JSON 实现
└── *_test.go              # 测试文件
```

**设计原则**：完全扁平化设计，所有公开 API 和实现都在根目录，序列化器作为内部子包

## 快速开始

```go
import "github.com/ceyewan/genesis/cache"
```

### 基础使用

```go
// 1. 创建连接器
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()
redisConn.Connect(ctx)

// 2. 创建缓存组件
cacheClient, _ := cache.New(redisConn, &cache.Config{
    Prefix:     "myapp:",
    Serializer: "json",
}, cache.WithLogger(logger))

// 3. 基础缓存操作
err := cacheClient.Set(ctx, "user:1001", user, time.Hour)
var cachedUser User
err = cacheClient.Get(ctx, "user:1001", &cachedUser)
```

## 核心接口

### Cache 接口

```go
type Cache interface {
    // --- Key-Value ---
    Set(ctx context.Context, key string, value any, ttl time.Duration) error
    Get(ctx context.Context, key string, dest any) error
    Delete(ctx context.Context, key string) error
    Has(ctx context.Context, key string) (bool, error)
    Expire(ctx context.Context, key string, ttl time.Duration) error

    // --- Hash ---
    HSet(ctx context.Context, key string, field string, value any) error
    HGet(ctx context.Context, key string, field string, dest any) error
    HGetAll(ctx context.Context, key string, destMap any) error
    HDel(ctx context.Context, key string, fields ...string) error
    HIncrBy(ctx context.Context, key string, field string, increment int64) (int64, error)

    // --- Sorted Set ---
    ZAdd(ctx context.Context, key string, score float64, member any) error
    ZRem(ctx context.Context, key string, members ...any) error
    ZScore(ctx context.Context, key string, member any) (float64, error)
    ZRange(ctx context.Context, key string, start, stop int64, destSlice any) error
    ZRevRange(ctx context.Context, key string, start, stop int64, destSlice any) error
    ZRangeByScore(ctx context.Context, key string, min, max float64, destSlice any) error

    // --- List ---
    LPush(ctx context.Context, key string, values ...any) error
    RPush(ctx context.Context, key string, values ...any) error
    LPop(ctx context.Context, key string, dest any) error
    RPop(ctx context.Context, key string, dest any) error
    LRange(ctx context.Context, key string, start, stop int64, destSlice any) error
    LPushCapped(ctx context.Context, key string, limit int64, values ...any) error

    // --- Utility ---
    Close() error
}
```

## 配置设计

### Config 结构

```go
type Config struct {
    // Prefix: 全局 Key 前缀 (e.g., "app:v1:")
    Prefix string `json:"prefix" yaml:"prefix"`

    // RedisConnectorName: 使用的 Redis 连接器名称 (e.g., "default")
    RedisConnectorName string `json:"redis_connector_name" yaml:"redis_connector_name"`

    // Serializer: "json" | "msgpack"
    Serializer string `json:"serializer" yaml:"serializer"`
}
```

**序列化器支持**：

- `json`: JSON 序列化（默认）
- `msgpack`: MessagePack 序列化（计划中）

## 使用模式

### 1. Key-Value 缓存（最常用）

适用于对象缓存、会话存储等：

```go
// 缓存用户信息
user := User{ID: 1001, Name: "Alice", Email: "alice@example.com"}
err := cacheClient.Set(ctx, "user:1001", user, time.Hour)
if err != nil {
    return err
}

// 获取用户信息
var cachedUser User
err = cacheClient.Get(ctx, "user:1001", &cachedUser)
if err != nil {
    return err
}

// 检查是否存在
exists, err := cacheClient.Has(ctx, "user:1001")
if err != nil {
    return err
}
if exists {
    fmt.Printf("User exists: %+v\n", cachedUser)
}
```

### 2. Hash 操作

适用于用户属性、配置项等结构化数据：

```go
// 存储用户属性
err := cacheClient.HSet(ctx, "user:1001:profile", "name", "Alice")
err = cacheClient.HSet(ctx, "user:1001:profile", "email", "alice@example.com")
err = cacheClient.HSet(ctx, "user:1001:profile", "age", 25)

// 获取单个属性
var name string
err = cacheClient.HGet(ctx, "user:1001:profile", "name", &name)

// 获取所有属性
var profile map[string]string
err = cacheClient.HGetAll(ctx, "user:1001:profile", &profile)

// 计数器操作
newCount, err := cacheClient.HIncrBy(ctx, "user:1001:stats", "login_count", 1)
```

### 3. 排序集合操作

适用于排行榜、计分板、时间线等：

```go
// 更新游戏排行榜
err := cacheClient.ZAdd(ctx, "leaderboard", 95.5, "user:1001")
err = cacheClient.ZAdd(ctx, "leaderboard", 87.3, "user:1002")
err = cacheClient.ZAdd(ctx, "leaderboard", 92.1, "user:1003")

// 获取前 10 名（降序）
var topUsers []string
err = cacheClient.ZRevRange(ctx, "leaderboard", 0, 9, &topUsers)

// 获取特定分数范围的用户
var highScores []string
err = cacheClient.ZRangeByScore(ctx, "leaderboard", 90.0, 100.0, &highScores)

// 查询用户分数
score, err := cacheClient.ZScore(ctx, "leaderboard", "user:1001")
```

### 4. 列表操作

适用于消息队列、时间线、日志记录等：

```go
// 定长列表 - 只保留最近 100 条日志
type LogEntry struct {
    Level   string    `json:"level"`
    Message string    `json:"message"`
    Time    time.Time `json:"time"`
}

entry := LogEntry{
    Level:   "INFO",
    Message: "System started",
    Time:    time.Now(),
}
err := cacheClient.LPushCapped(ctx, "logs", 100, entry)

// 获取最近 20 条日志
var logs []LogEntry
err = cacheClient.LRange(ctx, "logs", 0, 19, &logs)

// 消息队列模式
err = cacheClient.RPush(ctx, "task_queue", task1, task2, task3)
var task Task
err = cacheClient.LPop(ctx, "task_queue", &task)
```

## 函数式选项

```go
// WithLogger 注入日志记录器
cacheClient, err := cache.New(redisConn, cfg, cache.WithLogger(logger))

// WithMeter 注入指标收集器
cacheClient, err := cache.New(redisConn, cfg, cache.WithMeter(meter))

// 组合使用
cacheClient, err := cache.New(redisConn, cfg,
    cache.WithLogger(logger),
    cache.WithMeter(meter))
```

## 资源所有权模型

Cache 组件采用**借用模型 (Borrowing Model)**：

1. **连接器 (Owner)**：拥有底层连接，负责创建连接池并在应用退出时执行 `Close()`
2. **Cache 组件 (Borrower)**：借用连接器中的客户端，不拥有其生命周期
3. **生命周期控制**：使用 `defer` 确保关闭顺序与创建顺序相反（LIFO）

```go
// ✅ 正确示例
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close() // 应用结束时关闭底层连接
redisConn.Connect(ctx)

cacheClient, _ := cache.New(redisConn, &cfg.Cache, cache.WithLogger(logger))
// cacheClient.Close() 为 no-op，但建议调用以保持接口一致性
```

## 与其他组件配合

```go
func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建连接器
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()
    redisConn.Connect(ctx)

    // 2. 创建缓存组件
    cacheClient, _ := cache.New(redisConn, &cfg.Cache, cache.WithLogger(logger))

    // 3. 使用缓存组件
    userService := service.NewUserService(cacheClient)
    leaderboardService := service.NewLeaderboardService(cacheClient)

    // 在业务代码中使用
    user, err := userService.GetUser(ctx, userID)
    topUsers, err := leaderboardService.GetTopUsers(ctx, 10)
}
```

## 最佳实践

1. **Key 命名**：使用冒号分隔的层次结构，如 `user:1001:profile`
2. **TTL 设置**：为所有缓存设置合理的过期时间，避免内存泄漏
3. **序列化选择**：JSON 适合人类可读，MessagePack 适合高性能场景
4. **批量操作**：优先使用批量接口减少网络往返
5. **错误处理**：使用 `xerrors.Wrapf()` 包装错误，保留错误链
6. **连接管理**：使用 `WithLogger` 和 `WithMeter` 注入可观测性组件

## 完整示例

```go
package main

import (
    "context"
    "encoding/json"
    "time"

    "github.com/ceyewan/genesis/cache"
    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/connector"
)

type User struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

type LeaderboardEntry struct {
    UserID    string  `json:"user_id"`
    Score     float64 `json:"score"`
    Rank      int     `json:"rank"`
    UpdatedAt time.Time `json:"updated_at"`
}

func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建 Redis 连接器
    redisConn, err := connector.NewRedis(&connector.RedisConfig{
        Addr:         "127.0.0.1:6379",
        Password:     "",
        DB:           0,
        DialTimeout:  2 * time.Second,
        ReadTimeout:  3 * time.Second,
        WriteTimeout: 3 * time.Second,
        PoolSize:     10,
    }, connector.WithLogger(logger))
    if err != nil {
        panic(err)
    }
    defer redisConn.Close()

    // 2. 连接到 Redis
    if err := redisConn.Connect(ctx); err != nil {
        panic(err)
    }

    // 3. 创建缓存组件
    cacheClient, err := cache.New(redisConn, &cache.Config{
        Prefix:     "gameapp:",
        Serializer: "json",
    }, cache.WithLogger(logger))
    if err != nil {
        panic(err)
    }

    // 4. 用户缓存示例
    user := User{ID: 1001, Name: "Alice", Email: "alice@example.com"}
    err = cacheClient.Set(ctx, "user:1001", user, time.Hour)
    if err != nil {
        panic(err)
    }

    var cachedUser User
    err = cacheClient.Get(ctx, "user:1001", &cachedUser)
    if err != nil {
        panic(err)
    }
    logger.Info("cached user", clog.Int64("id", cachedUser.ID), clog.String("name", cachedUser.Name))

    // 5. 排行榜示例
    err = cacheClient.ZAdd(ctx, "leaderboard", 95.5, "user:1001")
    err = cacheClient.ZAdd(ctx, "leaderboard", 87.3, "user:1002")
    err = cacheClient.ZAdd(ctx, "leaderboard", 92.1, "user:1003")

    var topUsers []string
    err = cacheClient.ZRevRange(ctx, "leaderboard", 0, 2, &topUsers)
    if err != nil {
        panic(err)
    }
    logger.Info("top users", clog.Any("users", topUsers))

    // 6. 用户属性示例
    err = cacheClient.HSet(ctx, "user:1001:stats", "login_count", 42)
    err = cacheClient.HSet(ctx, "user:1001:stats", "last_login", time.Now().Format(time.RFC3339))
    err = cacheClient.HSet(ctx, "user:1001:stats", "level", 15)

    var stats map[string]string
    err = cacheClient.HGetAll(ctx, "user:1001:stats", &stats)
    if err != nil {
        panic(err)
    }
    logger.Info("user stats", clog.Any("stats", stats))

    // 7. 列表示例 - 最近活动
    activities := []string{
        "completed_level_15",
        "earned_achievement_speedrun",
        "defeated_boss_dragon",
    }
    err = cacheClient.LPushCapped(ctx, "user:1001:activities", 50, activities)

    var recentActivities []string
    err = cacheClient.LRange(ctx, "user:1001:activities", 0, 4, &recentActivities)
    if err != nil {
        panic(err)
    }
    logger.Info("recent activities", clog.Any("activities", recentActivities))

    logger.Info("Cache example completed successfully")
}
```
