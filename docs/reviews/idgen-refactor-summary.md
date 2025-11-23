# IDGen 组件 Option 模式重构总结

## 概述

本次重构为 IDGen 组件引入了 Option 模式，支持可观测性组件（Logger, Meter, Tracer）的依赖注入，并保持了与其他组件（Cache, MQ, DB, DLock）的一致性。

## 主要改动

### 1. 新增文件

#### `pkg/idgen/options.go`
- 定义组件初始化 Option 模式
- 提供 `WithLogger`, `WithMeter`, `WithTracer` 选项函数

```go
type Option func(*Options)

type Options struct {
    Logger clog.Logger
    Meter  telemetrytypes.Meter
    Tracer telemetrytypes.Tracer
}
```

### 2. 更新文件

#### `pkg/idgen/idgen.go`
**变更**:
- 新增类型导出：`SnowflakeConfig`, `UUIDConfig`
- 更新工厂函数签名，支持 Option 参数
- 添加详细的文档注释

**新签名**:
```go
func NewSnowflake(
    cfg *SnowflakeConfig,
    redisConn connector.RedisConnector,
    etcdConn connector.EtcdConnector,
    opts ...Option,
) (Int64Generator, error)

func NewUUID(
    cfg *UUIDConfig,
    opts ...Option,
) (Generator, error)
```

#### `internal/idgen/factory.go`
**变更**:
- 接受 Logger, Meter, Tracer 参数
- 为 Logger 添加 `component=idgen` 字段
- 传递可观测性组件到内部实现

#### `internal/idgen/snowflake/snowflake.go`
**变更**:
- 新增 `logger`, `meter`, `tracer` 字段
- 在 `Init()` 方法中添加日志记录
- 记录 WorkerID 分配成功/失败事件

#### `internal/idgen/uuid/uuid.go`
**变更**:
- 新增 `logger`, `meter`, `tracer` 字段
- 在创建时记录 UUID 版本信息

#### `internal/idgen/allocator/redis.go`
**变更**:
- 新增 `logger` 字段
- 在 `Start()` 方法中记录保活启动/停止/失败事件
- 熔断时记录详细错误信息

#### `internal/idgen/allocator/etcd.go`
**变更**:
- 新增 `logger` 字段
- 在 `Start()` 方法中记录保活启动/停止/失败事件
- 记录 Lease ID 信息

### 3. 更新文档

#### `docs/idgen-design.md`
**新增章节**:
- **6. 组件初始化选项 (Option Pattern)**: 详细说明 Option 模式的使用
- **6.1. 独立模式 (Standalone Mode)**: Snowflake 和 UUID 的独立使用示例
- **6.2. 容器模式 (Container Mode)**: Container 集成示例
- **7. 日志与可观测性**: Logger Namespace 和关键日志事件说明

## 使用示例

### 独立模式

```go
// Snowflake (Redis 模式)
gen, _ := idgen.NewSnowflake(&idgen.SnowflakeConfig{
    Method:       "redis",
    DatacenterID: 1,
    KeyPrefix:    "myapp:idgen:",
}, redisConn, nil, idgen.WithLogger(logger))

id, _ := gen.Int64()

// UUID
gen, _ := idgen.NewUUID(&idgen.UUIDConfig{
    Version: "v7",
}, idgen.WithLogger(logger))

uuid := gen.String()
```

### 容器模式

```go
app, _ := container.New(&container.Config{
    Redis: &connector.RedisConfig{
        Addr: "localhost:6379",
    },
    IDGen: &idgen.Config{
        Mode: "snowflake",
        Snowflake: &idgen.SnowflakeConfig{
            Method:       "redis",
            DatacenterID: 1,
        },
    },
}, container.WithLogger(logger))

id, _ := app.IDGen.Int64()
```

## 日志示例

```
level=info msg="worker id allocated" namespace=myapp component=idgen worker_id=123 datacenter_id=1
level=info msg="starting worker id keep alive" namespace=myapp component=idgen worker_id=123 key=myapp:idgen:123
level=error msg="keep alive failed, circuit breaking" namespace=myapp component=idgen worker_id=123 error="connection refused"
```

## 与其他组件的一致性

| 组件 | 组件级 Option | 操作级 Option | 文件结构 | Logger Namespace |
|------|--------------|--------------|---------|-----------------|
| Cache | ✅ | ❌ | ✅ 一致 | ✅ component=cache |
| MQ | ✅ | ❌ | ✅ 一致 | ✅ component=mq |
| DB | ✅ | ❌ | ✅ 一致 | ✅ component=db |
| DLock | ✅ | ✅ WithTTL | ✅ 一致 | ✅ component=dlock |
| IDGen | ✅ | ❌ | ✅ 一致 | ✅ component=idgen |

## 验证

- ✅ 所有文件编译通过
- ✅ 文件结构与其他组件一致
- ✅ 设计文档已更新
- ✅ 示例代码保持兼容（向后兼容）

## 注意事项

1. **向后兼容**: 旧的调用方式仍然有效（不传 Option 参数）
2. **默认 Logger**: 如果不传 Logger，会使用 `clog.Default()`
3. **Namespace 派生**: Logger 会自动添加 `component=idgen` 字段
4. **多种分配策略**: Static, IP, Redis, Etcd 四种 WorkerID 分配策略都已支持 Logger

