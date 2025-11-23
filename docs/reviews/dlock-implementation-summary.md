# DLock 组件实现总结

## 1. 概述

DLock（分布式锁）组件已按照设计文档和组件开发规范完成重构，支持**双模式**（独立模式 + 容器模式），并引入 **Option 模式**实现依赖注入。

## 2. 核心改进 ✅

### 2.1 引入组件级 Option 模式

**新增文件**: `pkg/dlock/options.go`

```go
type Option func(*Options)

type Options struct {
    Logger clog.Logger
    Meter  types.Meter
    Tracer types.Tracer
}

func WithLogger(l clog.Logger) Option
func WithMeter(m types.Meter) Option
func WithTracer(t types.Tracer) Option
```

**注意区分**:

- **组件级 Option** (`pkg/dlock/options.go`): 用于组件初始化，注入 Logger, Meter, Tracer
- **Lock 操作 Option** (`pkg/dlock/types/options.go`): 用于 Lock/TryLock 操作，设置 TTL 等参数

### 2.2 双模式支持

**独立模式 (Standalone)**:

```go
// Redis 后端
redisConn, _ := connector.NewRedis(redisConfig)
locker, _ := dlock.NewRedis(redisConn, &dlock.Config{
    Prefix: "myapp:lock:",
    DefaultTTL: 30 * time.Second,
}, dlock.WithLogger(logger))

// Etcd 后端
etcdConn, _ := connector.NewEtcd(etcdConfig)
locker, _ := dlock.NewEtcd(etcdConn, &dlock.Config{
    Prefix: "myapp:lock:",
    DefaultTTL: 30 * time.Second,
}, dlock.WithLogger(logger))
```

**容器模式 (Container)**:

```go
app, _ := container.New(&container.Config{
    Redis: redisConfig,
    DLock: &dlock.Config{
        Backend: dlock.BackendRedis,
        Prefix: "myapp:lock:",
        DefaultTTL: 30 * time.Second,
    },
}, container.WithLogger(logger))

// 直接使用
app.DLock.Lock(ctx, "resource:123")
defer app.DLock.Unlock(ctx, "resource:123")
```

## 3. 架构变更

### 3.1 文件结构

```
pkg/dlock/
├── dlock.go          # 工厂函数 (NewRedis, NewEtcd) + 类型导出
├── options.go        # 组件级 Option 模式定义 (新增)
└── types/
    ├── config.go     # 配置结构体
    ├── interface.go  # Locker 接口
    └── options.go    # Lock 操作 Option (WithTTL)

internal/dlock/
├── factory.go        # 内部工厂 (更新签名)
├── redis/
│   └── locker.go     # Redis 实现 (新增 meter, tracer)
└── etcd/
    └── locker.go     # Etcd 实现 (新增 meter, tracer)
```

### 3.2 签名变更

**pkg/dlock/dlock.go**:

```go
// 旧签名
func NewRedis(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger) (Locker, error)
func NewEtcd(conn connector.EtcdConnector, cfg *types.Config, logger clog.Logger) (Locker, error)

// 新签名
func NewRedis(conn connector.RedisConnector, cfg *Config, opts ...Option) (Locker, error)
func NewEtcd(conn connector.EtcdConnector, cfg *Config, opts ...Option) (Locker, error)
```

**internal/dlock/factory.go**:

```go
// 旧签名
func NewRedis(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger) (types.Locker, error)
func NewEtcd(conn connector.EtcdConnector, cfg *types.Config, logger clog.Logger) (types.Locker, error)

// 新签名
func NewRedis(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) (types.Locker, error)
func NewEtcd(conn connector.EtcdConnector, cfg *types.Config, logger clog.Logger, meter telemetrytypes.Meter, tracer telemetrytypes.Tracer) (types.Locker, error)
```

## 4. Container 集成

```go
// pkg/container/container.go
func (c *Container) initDLock(cfg *Config) error {
    switch cfg.DLock.Backend {
    case dlocktypes.BackendRedis:
        redisConn, _ := c.GetRedisConnector(*cfg.Redis)
        
        locker, _ := dlock.NewRedis(redisConn, cfg.DLock,
            dlock.WithLogger(c.Log),
            dlock.WithMeter(c.Meter),
            dlock.WithTracer(c.Tracer),
        )
        
        c.DLock = locker
        
    case dlocktypes.BackendEtcd:
        etcdConn, _ := c.GetEtcdConnector(*cfg.Etcd)
        
        locker, _ := dlock.NewEtcd(etcdConn, cfg.DLock,
            dlock.WithLogger(c.Log),
            dlock.WithMeter(c.Meter),
            dlock.WithTracer(c.Tracer),
        )
        
        c.DLock = locker
    }
    
    return nil
}
```

## 5. 使用示例

### 5.1 独立模式

```go
// 创建 Redis 连接器
redisConn, _ := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
})

// 创建 DLock 实例
locker, _ := dlock.NewRedis(redisConn, &dlock.Config{
    Prefix: "myapp:lock:",
    DefaultTTL: 30 * time.Second,
    RetryInterval: 100 * time.Millisecond,
}, dlock.WithLogger(logger))

// 使用锁
ctx := context.Background()
if err := locker.Lock(ctx, "order:123", dlock.WithTTL(10*time.Second)); err != nil {
    log.Fatal(err)
}
defer locker.Unlock(ctx, "order:123")

// 业务逻辑
processOrder()
```

### 5.2 容器模式

```go
app, _ := container.New(&container.Config{
    Redis: &connector.RedisConfig{
        Addr: "localhost:6379",
    },
    DLock: &dlock.Config{
        Backend: dlock.BackendRedis,
        Prefix: "myapp:lock:",
        DefaultTTL: 30 * time.Second,
    },
}, container.WithLogger(logger))

// 直接使用
ctx := context.Background()
app.DLock.Lock(ctx, "order:123")
defer app.DLock.Unlock(ctx, "order:123")
```

## 6. 设计原则遵循

- ✅ **依赖注入**: 通过 Option 模式注入 Logger, Meter, Tracer
- ✅ **配置分离**: 配置通过结构体参数接收
- ✅ **双模式支持**: 独立模式 + 容器模式
- ✅ **可观测性优先**: 支持 Logger, Meter, Tracer 注入
- ✅ **接口驱动**: pkg/ 定义接口，internal/ 提供实现
- ✅ **Namespace 派生**: Logger 自动追加 "dlock" Namespace
- ✅ **多后端支持**: Redis 和 Etcd 两种后端

## 7. 与其他组件的一致性

| 特性 | Cache | MQ | DB | DLock |
|------|-------|----|----|-------|
| Option 模式 | ✅ | ✅ | ✅ | ✅ |
| 双模式支持 | ✅ | ✅ | ✅ | ✅ |
| Logger 注入 | ✅ | ✅ | ✅ | ✅ |
| Meter 注入 | ✅ | ✅ | ✅ | ✅ |
| Tracer 注入 | ✅ | ✅ | ✅ | ✅ |
| Namespace 派生 | ✅ | ✅ | ✅ | ✅ |
| Container 集成 | ✅ | ✅ | ✅ | ✅ |
| 多后端支持 | - | ✅ | - | ✅ |

## 8. 参考文档

- [DLock 设计文档](../dlock-design.md)
- [Genesis 宏观设计](../genesis-design.md)
- [组件开发规范](../specs/component-spec.md)
- [Container 设计文档](../container-design.md)
