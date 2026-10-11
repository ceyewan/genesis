# cache - 缓存组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/cache.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/cache)

`cache` 是 Genesis 的 L2 业务层组件，提供三类缓存入口：

- `Distributed`：分布式缓存，当前基于 Redis，支持 `KV + Hash + Sorted Set + Batch`。
- `Local`：本地缓存，当前基于进程内存，只提供稳定的 `KV` 语义。
- `Multi`：多级缓存，组合 `Local` 与 `Distributed`，提供两级 `KV` 策略。

完整 API 与行为契约见 `go doc -all ./cache` 和本页；旧设计博客仅供历史参考。

## 快速开始

### 分布式缓存

```go
redisConn, err := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
if err != nil {
    return err
}
defer redisConn.Close()

if err := redisConn.Connect(ctx); err != nil {
    return err
}

dist, err := cache.NewDistributed(&cache.DistributedConfig{
    Driver:     cache.DriverRedis,
    KeyPrefix:  "myapp:",
    Serializer: "json",
}, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))
if err != nil {
    return err
}

if err := dist.Set(ctx, "user:1001", user, time.Hour); err != nil {
    return err
}

var cachedUser User
if err := dist.Get(ctx, "user:1001", &cachedUser); err != nil {
    return err
}
```

### 本地缓存

```go
local, err := cache.NewLocal(&cache.LocalConfig{
    Driver:     cache.DriverOtter,
    MaxEntries: 10000,
    Serializer: "json",
}, cache.WithLogger(logger))
if err != nil {
    return err
}
defer local.Close()

if err := local.Set(ctx, "profile:1001", profile, time.Minute); err != nil {
    return err
}
```

### 多级缓存

```go
multi, err := cache.NewMulti(local, dist, &cache.MultiConfig{
    BackfillTTL: time.Minute,
})
if err != nil {
    return err
}

if err := multi.Set(ctx, "user:1001", user, 10*time.Minute); err != nil {
    return err
}
```

## TTL 与错误语义

- `Set(..., ttl > 0)` / `Expire(..., ttl > 0)`：使用显式 TTL。
- `Set(..., ttl == 0)` / `Expire(..., ttl == 0)`：使用组件配置中的 `DefaultTTL`。
- `ttl < 0`：返回 `ErrInvalidTTL`，不会写入或修改缓存。
- `Get`、`HGet`、`ZScore` 等未命中时返回 `ErrMiss`。
- `HGetAll` 的目标必须是 `*map[string]T`；目标类型无效时返回 `ErrInvalidDestination`。
- `Has` 不返回 `ErrMiss`，而是通过布尔值表达存在性。
- `Expire` 返回 `(bool, error)`，其中 `bool=false` 表示 key 不存在。

## 配置

### DistributedConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Driver` | `DistributedDriverType` | `"redis"` | 后端驱动类型，当前仅支持 `"redis"` |
| `KeyPrefix` | `string` | `""` | 全局 key 前缀，用于多租户或命名空间隔离 |
| `Serializer` | `string` | `"json"` | 序列化器，支持 `"json"` 和 `"msgpack"` |
| `DefaultTTL` | `time.Duration` | `24h` | `ttl=0` 时的兜底 TTL |

### LocalConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Driver` | `LocalDriverType` | `"otter"` | 后端驱动类型，当前仅支持 `"otter"` |
| `MaxEntries` | `int` | `10000` | 缓存最大条目数，超出后 LRU 淘汰 |
| `Serializer` | `string` | `"json"` | 序列化器，支持 `"json"` 和 `"msgpack"` |
| `DefaultTTL` | `time.Duration` | `1h` | `ttl=0` 时的兜底 TTL |

JSON 与 MessagePack 不是可直接互换的线上编码。切换 `Serializer` 时必须清理旧缓存，或同时切换 `KeyPrefix`/版本；否则新实例可能无法解码旧值。自定义编码通过 `cache.WithSerializer` 注入，并承担同样的版本迁移责任。

### MultiConfig

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `LocalTTL` | `time.Duration` | `0` | 写入本地缓存时的 TTL，`0` 表示跟随写入 TTL |
| `BackfillTTL` | `time.Duration` | `1min` | 从远端回填本地缓存时使用的 TTL |
| `FailOpenOnLocalError` | `*bool` | `true` | 本地缓存异常时是否继续访问远端；`nil` 视为 `true` |

## 多级缓存一致性边界

Multi 是允许陈旧读取的两级缓存策略，不是远端的同步镜像：

- `LocalTTL` 可以长于远端写入 TTL；`ttl=0` 分别使用两层自己的默认值。
- 读回填使用 `BackfillTTL`，不读取远端剩余 TTL，因此远端过期后本地仍可能命中。
- 其他实例或绕过 Multi 的更新/删除不广播本地失效；陈旧值最多保留到本地条目
  最近一次写入/回填的 TTL 结束，也可能提前被容量策略淘汰。
- 同一个 Multi 实例协调 Get 回填与 Set/Delete/Expire，避免慢回填覆盖已经完成的
  本实例写入或删除。不同 Multi 实例及直接操作底层缓存不共享这把协调锁。
- 协调锁允许并发读取，但写操作会等待正在进行的远端读取；它不是按 key 的锁，
  需要严格取消预算的高吞吐场景应测量竞争并设置远端 I/O 超时。

需要及时反映库存、权限等权威状态时，直接读取 Distributed 或数据库；不要依赖
Multi 提供跨实例强一致性。本轮不改变既有 TTL 策略，也不新增自动失效协议。

## 推荐实践

- 把 `Distributed` 作为共享缓存和结构化缓存入口。
- 把 `Local` 作为进程内热点数据和短路径优化层，使用完毕需调用 `Close()`。
- 把 `Multi` 视为两级缓存策略，而不是新的存储引擎。
- 优先使用明确 TTL，避免把 `DefaultTTL` 当成长期数据保留策略。
- 需要 Pipeline、Lua 或高级 Redis 命令时，再使用 `RawClient()`。

## 测试

```bash
go test ./cache/... -count=1
go test -race ./cache/... -count=1
```

集成测试通过 testcontainers 自动启动 Redis 容器，直接运行即可，无需手动配置 Docker 环境。

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/cache)
- [历史设计博客](../docs/genesis-cache-blog.md)
- [Genesis 文档目录](../docs/README.md)
