# cache - Genesis 缓存组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/cache.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/cache)

`cache` 提供基于 Redis 的缓存操作，支持 String、Hash、Sorted Set、List 等数据结构。

## 特性

- 支持 Redis / Memory 驱动
- 自动序列化（JSON）
- Key 前缀管理
- L2 (Business) 层组件

## 快速开始

```go
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()

cacheClient, _ := cache.New(&cache.Config{
    Driver:     cache.DriverRedis,
    Prefix:     "myapp:",
    Serializer: "json",
}, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))

cacheClient.Set(ctx, "user:1001", user, time.Hour)
cacheClient.Get(ctx, "user:1001", &cachedUser)
```

## 核心接口

### Cache

```go
type Cache interface {
    // Key-Value
    Set(ctx, key, value, ttl) error
    Get(ctx, key, dest) error
    Delete(ctx, key) error
    Has(ctx, key) (bool, error)
    Expire(ctx, key, ttl) error

    // Hash
    HSet(ctx, key, field, value) error
    HGet(ctx, key, field, dest) error
    HGetAll(ctx, key, destMap) error
    HDel(ctx, key, fields...) error
    HIncrBy(ctx, key, field, incr) (int64, error)

    // Sorted Set
    ZAdd(ctx, key, score, member) error
    ZRem(ctx, key, members...) error
    ZScore(ctx, key, member) (float64, error)
    ZRange/ZRevRange/ZRangeByScore(ctx, key, start, stop, dest) error

    // List
    LPush/RPush/LPop/RPop/LRange/LPushCapped(ctx, key, ...) error

    Close() error
}
```

## 配置

### Config

```go
type Config struct {
    Driver     DriverType        // redis | memory
    Prefix     string            // Key 前缀
    Serializer string            // json | msgpack
    Standalone *StandaloneConfig // memory 驱动配置
}
```

### Memory 驱动

```go
cacheClient, _ := cache.New(&cache.Config{
    Driver: cache.DriverMemory,
    Standalone: &cache.StandaloneConfig{
        Capacity: 10000,
    },
})
```

## 使用示例

```go
// Key-Value
cacheClient.Set(ctx, "user:1001", user, time.Hour)
cacheClient.Get(ctx, "user:1001", &user)

// Hash
cacheClient.HSet(ctx, "user:1001:profile", "name", "Alice")
cacheClient.HGet(ctx, "user:1001:profile", "name", &name)

// Sorted Set
cacheClient.ZAdd(ctx, "leaderboard", 95.5, "user:1001")
cacheClient.ZRevRange(ctx, "leaderboard", 0, 9, &topUsers)

// List
cacheClient.LPushCapped(ctx, "logs", 100, entry)
cacheClient.LRange(ctx, "logs", 0, 19, &logs)
```

参考 [examples/cache/main.go](../examples/cache/main.go)。
