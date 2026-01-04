# idgen - Genesis ID 生成组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/idgen.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/idgen)

`idgen` 是 Genesis 业务层的核心组件，提供高性能的分布式 ID 生成能力。

## 特性

- **统一 API 设计**：所有组件遵循 `New(cfg, opts...)` 模式
- **接口优先**：返回接口类型，便于测试和替换实现
- **后端可插拔**：通过 `cfg.Driver` 选择后端实现（Redis/Etcd）
- **安全健壮**：
    - Redis 实现使用 Lua 脚本保证原子性
    - Etcd 实现使用 Txn + Lease 机制，原生支持租约保活
    - 严格的 WorkerID 位宽校验，防止 ID 冲突
    - 时钟回拨保护

## 快速开始

```go
import "github.com/ceyewan/genesis/idgen"
```

### 1. UUID (最简单)

```go
// 生成 UUID v7 (时间排序，适合数据库主键)
id := idgen.UUID()
```

### 2. Snowflake (手动指定 WorkerID)

```go
// 创建 Snowflake 生成器
gen, err := idgen.NewGenerator(&idgen.GeneratorConfig{
    WorkerID:     1,  // [0, 1023]
    DatacenterID: 0,  // 可选，[0, 31]（启用后 WorkerID 范围变为 [0, 31]）
})
if err != nil {
    panic(err)
}

id := gen.Next()       // int64
idStr := gen.NextString() // string
```

### 3. Snowflake (Allocator 自动分配 WorkerID)

```go
// 创建 Allocator
allocator, err := idgen.NewAllocator(&idgen.AllocatorConfig{
    Driver:    "redis",
    KeyPrefix: "myapp:worker",
    MaxID:     512,  // [0, 512)
    TTL:       30,   // 租约 TTL 30 秒
}, idgen.WithRedisConnector(redisConn))
if err != nil {
    panic(err)
}

// 分配 WorkerID
workerID, err := allocator.Allocate(ctx)
if err != nil {
    panic(err)
}

// 启动保活 (在 goroutine 中运行)
go func() {
    if err := <-allocator.KeepAlive(ctx); err != nil {
        log.Fatal("WorkerID 租约丢失，停止服务")
    }
}()

// 优雅退出时释放
defer allocator.Stop()

// 使用分配的 WorkerID 创建 Snowflake
gen, err := idgen.NewGenerator(&idgen.GeneratorConfig{WorkerID: workerID})
```

### 4. Allocator (Etcd)

Etcd 使用原生的 Txn + Lease 机制，性能优于 Redis Lua 方案：

```go
// 创建 Etcd Allocator
allocator, err := idgen.NewAllocator(&idgen.AllocatorConfig{
    Driver:    "etcd",
    KeyPrefix: "myapp:worker",
    MaxID:     512,
    TTL:       30,
}, idgen.WithEtcdConnector(etcdConn))

workerID, err := allocator.Allocate(ctx)
// ... 用法与 Redis 相同
```

**对比**:
| 特性 | Redis (Lua) | Etcd (Txn + Lease) |
|------|-------------|-------------------|
| 原子性 | Lua 脚本 | MVCC 事务 |
| 保活 | 后台 goroutine + EXPIRE | 原生 KeepAlive |
| 释放 | DEL | Lease revoke |
| 性能 | 需脚本加载 | 原生操作 |

### 5. Sequencer (分布式序列号)

基于 Redis/Etcd 的原子递增序列号，支持步长、TTL 和最大值循环。

```go
// 创建 Sequencer (Redis)
seq, err := idgen.NewSequencer(&idgen.SequencerConfig{
    Driver:    "redis",
    KeyPrefix: "order:seq",
    Step:      1,
    TTL:       86400, // 24 小时过期（秒）
}, idgen.WithRedisConnector(redisConn))
if err != nil {
    panic(err)
}

// 获取序列号 (支持动态 Key)
id, err := seq.Next(ctx, "20231224") // Redis Key: order:seq:20231224

// 批量获取
ids, err := seq.NextBatch(ctx, "batch:1", 10)

// 设置初始值（IM 系统迁移历史消息时很有用）
ok, err := seq.SetIfNotExists(ctx, "conversation:1", 1000)
```

## API 参考

### UUID

| 函数 | 说明 |
| :--- | :--- |
| `idgen.UUID()` | 生成 UUID v7 字符串（时间排序） |

### Generator (Snowflake)

| 方法 | 说明 |
| :--- | :--- |
| `idgen.NewGenerator(cfg, opts...)` | 创建 Snowflake 生成器（返回 Generator 接口） |
| `gen.Next()` | 获取下一个 ID (int64) |
| `gen.NextString()` | 获取下一个 ID (string) |

**GeneratorConfig**:
- `WorkerID`: 工作节点 ID [0, 1023]（使用 DatacenterID 时范围缩减为 [0, 31]）
- `DatacenterID`: 数据中心 ID [0, 31]，可选

### Allocator (WorkerID 分配器)

| 方法 | 说明 |
| :--- | :--- |
| `idgen.NewAllocator(cfg, opts...)` | 创建分配器（支持 Redis/Etcd） |
| `allocator.Allocate(ctx)` | 分配 WorkerID |
| `allocator.KeepAlive(ctx)` | 保持租约（返回 <-chan error） |
| `allocator.Stop()` | 释放资源 |

**AllocatorConfig**:
- `Driver`: 后端类型，"redis" 或 "etcd"
- `KeyPrefix`: 键前缀，默认 "genesis:idgen:worker"
- `MaxID`: 最大 ID 范围 [0, maxID)，默认 1024
- `TTL`: 租约 TTL（秒），默认 30

### Sequencer (序列号生成器)

| 方法 | 说明 |
| :--- | :--- |
| `idgen.NewSequencer(cfg, opts...)` | 创建序列号生成器（支持 Redis/Etcd） |
| `seq.Next(ctx, key)` | 获取下一个序列号 |
| `seq.NextBatch(ctx, key, count)` | 批量获取序列号 |
| `seq.Set(ctx, key, value)` | 设置序列号值 |
| `seq.SetIfNotExists(ctx, key, value)` | 仅当不存在时设置 |

**SequencerConfig**:
- `Driver`: 后端类型，"redis" 或 "etcd"，默认 "redis"
- `KeyPrefix`: 键前缀
- `Step`: 步长，默认 1
- `MaxValue`: 最大值（0 表示不限制）
- `TTL`: 过期时间（秒），0 表示永不过期

### 选项函数

所有组件支持统一的选项注入：

| 选项 | 说明 |
| :--- | :--- |
| `idgen.WithLogger(logger)` | 设置 Logger |
| `idgen.WithRedisConnector(conn)` | 注入 Redis 连接器 |
| `idgen.WithEtcdConnector(conn)` | 注入 Etcd 连接器 |

## 最佳实践

1. **WorkerID 管理**:
   - K8s StatefulSet: 直接使用 Pod 序号作为 WorkerID
   - 无状态 Deployment: 使用 Allocator 自动分配

2. **Allocator 保活监控**:
   ```go
   go func() {
       if err := <-allocator.KeepAlive(ctx); err != nil {
           // 租约丢失，停止服务以避免 ID 冲突
           log.Fatal("WorkerID lease lost")
       }
   }()
   ```

3. **IM 系统序列号初始化**:
   ```go
   // 迁移历史消息后，初始化 seq_id
   ok, _ := seq.SetIfNotExists(ctx, "conversation:1", maxHistorySeqID)
   ```

4. **优雅退出**:
   ```go
   defer allocator.Stop()  // 释放 WorkerID
   ```
