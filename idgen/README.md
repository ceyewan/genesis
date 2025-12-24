# idgen - Genesis ID 生成组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/idgen.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/idgen)

`idgen` 是 Genesis 业务层的核心组件，提供高性能的分布式 ID 生成能力。经过 v0.2 重构，它现在更加轻量、解耦且易用。

## 特性

- **核心解耦**：Snowflake 算法与基础设施（Redis）完全分离，可独立使用。
- **多策略支持**：
    - **Snowflake**：基于雪花算法的分布式有序 ID 生成。
    - **UUID**：支持 v4（随机）和 v7（时间排序）标准。
    - **Sequencer**：基于 Redis 原子操作的分布式序列号生成器。
    - **InstanceID**：基于 Redis 租约的唯一节点 ID 分配器。
- **极简 API**：提供 `Next()`、`NextUUID()` 等静态便捷入口，零配置上手。
- **安全健壮**：
    - Sequencer 使用 Lua 脚本保证原子性，彻底解决并发竞争问题。
    - 严格的 WorkerID 位宽校验，防止 ID 冲突。
    - 时钟回拨保护。

## 快速开始

```go
import "github.com/ceyewan/genesis/idgen"
```

### 1. Snowflake (全局静态模式)

最简单的使用方式，适用于大多数场景。

```go
// 1. 初始化 (通常在 main.go 启动时)
// 传入当前节点的 WorkerID [0-1023]
if err := idgen.Setup(1); err != nil {
    panic(err)
}

// 2. 生成 ID
id := idgen.Next()       // int64: 1782348234234234
str := idgen.NextString() // string: "1782348234234234"
```

### 2. UUID (开箱即用)

```go
// 默认生成 v7 版本 (时间有序，适合数据库主键)
uid := idgen.NextUUID() 

// 显式生成
v4 := idgen.NewUUIDV4()
v7 := idgen.NewUUIDV7()
```

### 3. Sequencer (业务序列号)

基于 Redis 的原子递增序列号，支持 TTL 和最大值循环。

```go
// 初始化 Sequencer 实例
seqGen, _ := idgen.NewSequencer(&idgen.SequenceConfig{
    KeyPrefix: "order:seq",
    Step:      1,
    TTL:       int64(24 * time.Hour), // 每日重置
}, redisConn)

// 获取序列号 (支持动态 Key)
id, _ := seqGen.Next(ctx, "20231224") // Redis Key: order:seq:20231224
```

### 4. 自动分配 WorkerID (InstanceID)

如果不想手动管理 Snowflake 的 WorkerID，可以使用 `AssignInstanceID` 基于 Redis 自动抢占。

```go
// 1. 抢占一个唯一的 WorkerID [0, 1024)
workerID, stop, failCh, err := idgen.AssignInstanceID(ctx, redisConn, "myapp", 1024)
if err != nil {
    panic(err)
}
// 停止保活 (优雅退出时调用)
defer stop()

// 监听保活失败 (可选但推荐)
go func() {
    if err := <-failCh; err != nil {
        log.Fatal("WorkerID 租约丢失，停止服务")
    }
}()

// 2. 使用分配的 ID 初始化 Snowflake
idgen.Setup(workerID)
```

## API 参考

### Snowflake

| 方法 | 说明 |
| :--- | :--- |
| `idgen.Setup(workerID)` | 初始化全局单例 |
| `idgen.Next()` | 获取全局单例的下一个 ID (int64) |
| `idgen.NewSnowflake(workerID)` | 创建独立的 Snowflake 实例 |

**配置项**:
- `WithDatacenterID(id)`: 设置数据中心 ID (0-31)。注意：启用此项时，WorkerID 范围缩减为 0-31。
- `WithSnowflakeLogger(logger)`: 自定义日志记录器。

### Sequencer

| 方法 | 说明 |
| :--- | :--- |
| `idgen.NewSequencer(cfg, redis)` | 创建序列号生成器实例 |
| `idgen.NextSequence(ctx, redis, key)` | 便捷函数，使用默认配置生成序列号 |
| `seq.Next(ctx, key)` | 获取下一个序列号 |
| `seq.NextBatch(ctx, key, count)` | 批量获取序列号 |

**配置项 (SequenceConfig)**:
- `KeyPrefix`: Redis 键前缀
- `Step`: 步长 (默认 1)
- `MaxValue`: 最大值 (超过自动重置)
- `TTL`: 过期时间 (纳秒)

## 最佳实践

1.  **Snowflake 初始化**: 尽量在应用启动的最早阶段调用 `idgen.Setup()`。
2.  **WorkerID 管理**:
    - 对于 K8s StatefulSet，可以直接使用 Pod 序号作为 WorkerID。
    - 对于无状态 Deployment，推荐使用 `AssignInstanceID` 自动分配。
3.  **序列号原子性**: 
    - `Sequencer` 内部使用 Lua 脚本保证 `INCR` + `Check` + `Reset` 的原子性，在高并发下是安全的。
4.  **错误处理**: 
    - 使用 `AssignInstanceID` 时，务必监控 `failCh`。如果 Redis 连接断开导致租约失效，继续生成 ID 可能会导致与其他节点冲突。

## 迁移指南 (v0.1 -> v0.2)

v0.2 移除了对 Etcd 的支持，并简化了 Snowflake 的初始化流程。

**旧代码**:
```go
// 强依赖 Redis，且配置复杂
gen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
    Method: "redis", 
    KeyPrefix: "..."
}, redisConn, nil)
```

**新代码**:
```go
// 1. 分配 (可选)
wid, stop, _, _ := idgen.AssignInstanceID(ctx, redisConn, "...", 1024)
defer stop()

// 2. 初始化
idgen.Setup(wid)

// 3. 使用
id := idgen.Next()
```
