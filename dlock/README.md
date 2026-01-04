# dlock - 分布式锁组件

`dlock` 是 Genesis 的分布式锁组件，支持 Redis 和 Etcd 后端。

## 特性

- **后端无关**：支持 Redis、Etcd，通过配置灵活切换
- **自动续期**：Redis Watchdog / Etcd Session KeepAlive
- **防误删**：通过 token 机制确保只有锁持有者才能释放

## 目录结构

```
dlock/
├── README.md        # 组件文档
├── dlock.go         # 公开 API: New()
├── types.go         # Locker, Config, DriverType
├── options.go       # Option, WithLogger, With*Connector
├── lock_options.go  # LockOption, WithTTL
├── errors.go        # 标准错误定义
├── redis.go         # Redis 后端
└── etcd.go          # Etcd 后端
```

## 快速开始

### Redis

```go
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()

locker, _ := dlock.New(&dlock.Config{
    Driver:        dlock.DriverRedis,
    Prefix:        "myapp:lock:",
    DefaultTTL:    10 * time.Second,
    RetryInterval: 100 * time.Millisecond,
}, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))

ctx := context.Background()
if err := locker.Lock(ctx, "resource-key"); err != nil {
    return err
}
defer locker.Unlock(ctx, "resource-key")
```

### Etcd

```go
etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
defer etcdConn.Close()

locker, _ := dlock.New(&dlock.Config{
    Driver:     dlock.DriverEtcd,
    Prefix:     "myapp:lock:",
    DefaultTTL: 30 * time.Second,
}, dlock.WithEtcdConnector(etcdConn), dlock.WithLogger(logger))
```

## 核心接口

### Locker

```go
type Locker interface {
    Lock(ctx context.Context, key string, opts ...LockOption) error
    TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error)
    Unlock(ctx context.Context, key string) error
}
```

### Config

```go
type Config struct {
    Driver        DriverType   // redis | etcd
    Prefix        string       // 锁 Key 前缀，如 "myapp:lock:"
    DefaultTTL    time.Duration // 默认锁超时时间
    RetryInterval time.Duration // 加锁重试间隔
}
```

## 应用场景

### 任务竞选

```go
acquired, _ := locker.TryLock(ctx, "scheduled-task:cleanup")
if acquired {
    defer locker.Unlock(ctx, "scheduled-task:cleanup")
    runCleanup()
}
```

### 库存扣减

```go
locker.Lock(ctx, fmt.Sprintf("inventory:%d", productID),
    dlock.WithTTL(30*time.Second))
defer locker.Unlock(ctx, fmt.Sprintf("inventory:%d", productID))
```

## 可观测性

通过 `WithLogger` 注入日志器，自动添加 `component=dlock` 字段。

## 工厂函数

```go
func New(cfg *Config, opts ...Option) (Locker, error)
```

选项：`WithRedisConnector`、`WithEtcdConnector`、`WithLogger`。

## 标准错误

```go
var (
    ErrConfigNil       = xerrors.New("dlock: config is nil")
    ErrConnectorNil    = xerrors.New("dlock: connector is nil")
    ErrLockNotHeld     = xerrors.New("dlock: lock not held")
    ErrLockAlreadyHeld = xerrors.New("dlock: lock already held locally")
    ErrOwnershipLost   = xerrors.New("dlock: ownership lost")
)
```

## 完整示例

参考 [examples/dlock/main.go](../examples/dlock/main.go)。
