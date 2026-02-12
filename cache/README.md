# cache - 缓存组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/cache.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/cache)

统一的缓存抽象层，支持 Redis 和 Memory 两种驱动。

## 特性

| 特性       | Redis 驱动 | Memory 驱动 |
| ---------- | ---------- | ----------- |
| Key-Value  | ✅         | ✅          |
| Hash       | ✅         | ❌          |
| Sorted Set | ✅         | ❌          |
| List       | ✅         | ❌          |
| MSet/MGet  | ✅         | ❌          |

## 快速开始

### Redis 驱动

```go
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()

cache, _ := cache.New(&cache.Config{
    Prefix:     "myapp:",
    Serializer: "json",
}, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))

cache.Set(ctx, "user:1001", user, time.Hour)
cache.Get(ctx, "user:1001", &cachedUser)
```

### Memory 驱动

```go
cache, _ := cache.New(&cache.Config{
    Driver:     cache.DriverMemory,
    Standalone: &cache.StandaloneConfig{Capacity: 10000},
})

cache.Set(ctx, "key", "value", time.Minute)
```

## 核心 API

```go
type Cache interface {
    // Key-Value
    Set(ctx, key, value, ttl) error
    Get(ctx, key, dest) error
    Delete(ctx, key) error
    Has(ctx, key) (bool, error)
    Expire(ctx, key, ttl) error

    // Hash (仅 Redis)
    HSet/HGet/HGetAll/HDel/HIncrBy(ctx, key, ...) error

    // Sorted Set (仅 Redis)
    ZAdd/ZRem/ZScore/ZRange/ZRevRange/ZRangeByScore(ctx, key, ...) error

    // List (仅 Redis)
    LPush/RPush/LPop/RPop/LRange/LPushCapped(ctx, key, ...) error

    // Batch (仅 Redis)
    MGet(ctx, keys, dest) error
    MSet(ctx, items, ttl) error

    Close() error
}
```

## 配置

```go
type Config struct {
    Driver     DriverType        // redis | memory
    Prefix     string            // Key 前缀
    Serializer string            // json | msgpack
    Standalone *StandaloneConfig // memory 驱动配置
}

type StandaloneConfig struct {
    Capacity int // 最大条目数，默认 10000
}
```

## 测试

```bash
# 单元测试（无外部依赖）
go test -v ./cache -run Unit

# 集成测试（需要 Docker）
go test -v ./cache -run Integration
```

## 示例

参考 [examples/cache/main.go](../examples/cache/main.go)。
