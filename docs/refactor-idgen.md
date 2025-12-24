# Idgen 组件重构设计文档

## 1. 背景与目标

根据 Issue #16 及后续讨论，当前 `idgen` 组件存在 Snowflake 强耦合 Redis/Etcd、API 使用繁琐等问题。
本设计旨在将 `idgen` 重构为以 Redis 为默认中间件（移除 Etcd 依赖），但核心算法解耦的高易用性 ID 生成套件。

**重构目标**：
1.  **解耦**: Snowflake 算法不再负责 WorkerID 分配，移除 `allocator` 接口。
2.  **易用**: 提供统一的静态方法 (`Next`, `NextUUID`) 和便捷函数。
3.  **精简**: 移除 Etcd 依赖，专注 Redis 生态。
4.  **一致**: 遵循 "实例优先，静态辅助" 的 API 设计原则。

## 2. 架构调整

### 2.1 目录结构变更

```text
idgen/
├── idgen.go           # [修改] 静态 API 入口 (Next, Setup, AssignInstanceID, NextSequence)
├── snowflake.go       # [修改] 纯算法实现，移除 Allocator 依赖
├── uuid.go            # [修改] 纯算法实现
├── sequence.go        # [保留] 基于 Redis 的序列号生成器
├── options.go         # [修改] 清理与 Connector/Allocator 相关的配置
└── internal/          # [删除] 移除 allocator 及其子包
```

### 2.2 依赖关系

- **Core (Snowflake/UUID)**: 无外部依赖（仅依赖标准库）。
- **Extensions (InstanceID/Sequencer)**: 依赖 `connector` (Redis)。

## 3. API 详细设计

### 3.1 Snowflake (分布式 ID)

**核心变更**: `NewSnowflake` 不再接受 Connector，只接受 WorkerID。

```go
// 1. 实例模式 (推荐)
// workerID: [0, 1023] (取决于 DatacenterID 配置，默认 10 bit WorkerID)
sf, _ := idgen.NewSnowflake(workerID, opts...)
id := sf.NextInt64()

// 2. 全局静态模式 (便捷)
// 必须先调用 Setup 初始化全局单例
idgen.Setup(workerID, opts...)
id := idgen.Next()
```

### 3.2 UUID (唯一 ID)

提供 V7 (默认) 和 V4 支持。

```go
// 1. 静态模式 (最常用)
// 默认生成 UUID v7
uid := idgen.NextUUID() 

// 2. 实例模式
gen, _ := idgen.NewUUID(opts...)
uid := gen.Next()
```

### 3.3 InstanceID (WorkerID 分配)

将原有的 `internal/allocator` 逻辑重构为公开的辅助函数，用于在集群中抢占一个唯一的 WorkerID。

```go
// 基于 Redis 抢占分配唯一 ID [0, maxID)
// 返回: instanceID, stopFunc, error
// stopFunc 用于停止自动保活，通常在服务关闭时调用
func AssignInstanceID(ctx context.Context, redis connector.RedisConnector, key string, maxID int) (int64, func(), error)
```

### 3.4 Sequencer (业务序列号)

保留现有的 `NewSequencer`，并新增无配置的便捷函数。

```go
// 1. 实例模式 (标准，支持 TTL, MaxValue, Step 等配置)
// 适用于 Order, Message 等核心业务序列
seq, _ := idgen.NewSequencer(&idgen.SequenceConfig{
    KeyPrefix: "im:msg",
    Step: 1,
}, redisConn)
id, _ := seq.Next(ctx, "session_1001") // Redis Key: im:msg:session_1001

// 2. 静态便捷模式 (新增)
// 适用于临时或简单的计数需求 (Step=1, 无 TTL, 无 Prefix)
id, _ := idgen.NextSequence(ctx, redisConn, "simple_counter")
```

## 4. 迁移指南

### 4.1 场景：仅使用 Snowflake (无自动分配)

**Old:**
```go
// 强迫传 nil
gen, _ := idgen.NewSnowflake(&Config{Method: "static", WorkerID: 1}, nil, nil)
```

**New:**
```go
// 干净清爽
idgen.Setup(1)
id := idgen.Next()
```

### 4.2 场景：使用 Redis 自动分配 WorkerID

**Old:**
```go
gen, _ := idgen.NewSnowflake(&Config{Method: "redis"}, redisConn, nil)
```

**New:**
```go
// 显式分两步：先分配，再初始化
workerID, stop, err := idgen.AssignInstanceID(ctx, redisConn, "my-app", 1024)
if err != nil { ... }
defer stop()

idgen.Setup(workerID)
id := idgen.Next()
```

## 5. 实施计划

1.  **清理**: 删除 `internal/allocator` 及 Etcd 相关代码。
2.  **重构 Snowflake**: 修改构造函数签名，移除 Allocator 逻辑。
3.  **实现 InstanceID**: 将 Redis Allocator 逻辑迁移至 `idgen.go` 中的 `AssignInstanceID`。
4.  **增强 Sequence**: 添加 `NextSequence` 函数。
5.  **统一入口**: 在 `idgen.go` 中实现 `Setup`, `Next`, `NextUUID`。
6.  **测试与示例**: 更新单元测试和 `examples/idgen/main.go`。
