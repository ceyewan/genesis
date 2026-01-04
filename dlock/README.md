# dlock - 分布式锁组件

`dlock` (Distributed Lock) 是 Genesis 框架中的分布式锁组件，屏蔽底层存储差异，提供统一、安全且易用的分布式锁 API。

## 特性

- **后端无关**：支持 Redis、Etcd 等多种后端，通过配置灵活切换
- **接口抽象**：业务代码仅依赖 `Locker` 接口，不感知底层实现细节
- **自动续期**：Redis 使用 Watchdog 自动续期；Etcd 使用 Session KeepAlive 自动续期
- **防误删**：通过令牌机制确保只有锁的持有者才能释放
- **可观测性**：支持注入 Logger；Meter 目前仅保留扩展点
- **显式依赖注入**：不自行管理连接，而是依赖 `connector` 层提供的连接实例

## 目录结构

dlock 采用完全扁平化设计，所有文件直接位于包目录下：

```
dlock/
├── README.md         # 本文件：组件文档
├── dlock.go          # 公开 API: New()
├── types.go          # 类型定义: Locker, Config, DriverType
├── options.go        # 组件选项: Option, WithLogger(), WithMeter(), WithRedisConnector(), WithEtcdConnector()
├── lock_options.go   # Lock 操作: LockOption, WithTTL()
├── errors.go         # 错误定义: ErrConfigNil, ErrLockNotHeld 等
├── metrics.go        # 指标常量定义
├── redis.go          # Redis 后端实现
└── etcd.go           # Etcd 后端实现
```

## 快速开始

### 基础使用

```go
package main

import (
    "context"
    "time"

    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/config"
    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/dlock"
)

func main() {
    // 1. 初始化配置与日志
    cfg, _ := config.Load("config.yaml")
    logger, _ := clog.New(&cfg.Log)

    // 2. 创建 Redis 连接器
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()

    // 3. 创建分布式锁组件
    locker, _ := dlock.New(&dlock.Config{
        Driver:        dlock.DriverRedis,
        Prefix:        "myapp:lock:",
        DefaultTTL:    10 * time.Second,
        RetryInterval: 100 * time.Millisecond,
    }, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))

    // 4. 使用锁
    ctx := context.Background()

    // 阻塞式加锁
    if err := locker.Lock(ctx, "resource-key"); err != nil {
        logger.Error("failed to acquire lock", clog.Error(err))
        return
    }
    defer locker.Unlock(ctx, "resource-key")

    // 执行临界区代码
    logger.Info("critical section", clog.String("key", "resource-key"))
}
```

### Etcd 后端

```go
// 创建 Etcd 连接器
etcdConn, _ := connector.NewEtcd(&cfg.Etcd, connector.WithLogger(logger))
defer etcdConn.Close()

// 创建 Etcd 分布式锁
locker, _ := dlock.New(&dlock.Config{
    Driver:     dlock.DriverEtcd,
    Prefix:     "myapp:lock:",
    DefaultTTL: 30 * time.Second,
}, dlock.WithEtcdConnector(etcdConn), dlock.WithLogger(logger))
```

## 核心接口

### Locker 接口

```go
type Locker interface {
    // Lock 阻塞式加锁
    // 成功返回 nil，失败返回错误
    Lock(ctx context.Context, key string, opts ...LockOption) error

    // TryLock 非阻塞式尝试加锁
    // 成功返回 true, nil；锁已被占用返回 false, nil；出错返回 false, err
    TryLock(ctx context.Context, key string, opts ...LockOption) (bool, error)

    // Unlock 释放锁
    // 只有锁的持有者才能成功释放
    Unlock(ctx context.Context, key string) error
}
```

### Config 配置结构

```go
type Config struct {
    // Driver 选择使用的后端 (redis | etcd)
    Driver DriverType `json:"driver" yaml:"driver"`

    // Prefix 锁 Key 的全局前缀，例如 "myapp:lock:"
    Prefix string `json:"prefix" yaml:"prefix"`

    // DefaultTTL 默认锁超时时间
    // Redis 使用 Watchdog 自动续期；Etcd 使用 Session KeepAlive 自动续期
    DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`

    // RetryInterval 加锁重试间隔 (仅 Lock 模式有效)
    RetryInterval time.Duration `json:"retry_interval" yaml:"retry_interval"`
}
```

## 应用场景

### 分布式任务调度

确保在微服务集群中，同一时刻只有一个服务实例执行定时任务：

```go
// 定时任务竞选
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

acquired, _ := locker.TryLock(ctx, "scheduled-task:cleanup")
if acquired {
    defer locker.Unlock(context.Background(), "scheduled-task:cleanup")
    // 执行清理任务
}
```

### 库存扣减

电商系统中保护库存数据的一致性：

```go
// 长时间持有锁
ctx := context.Background()
locker.Lock(ctx, fmt.Sprintf("inventory:%d", productID),
    dlock.WithTTL(30*time.Second))
defer locker.Unlock(ctx, fmt.Sprintf("inventory:%d", productID))

// 读取库存、检查、扣减、写回
inventory := getInventory(productID)
if inventory > 0 {
    updateInventory(productID, inventory-1)
}
```

### 关键资源保护

确保数据库迁移、配置更新等关键操作的互斥性：

```go
locker.Lock(ctx, "database:migration:lock")
defer locker.Unlock(ctx, "database:migration:lock")

// 执行数据库迁移
runMigration()
```

## 可观测性

### 日志示例

```go
locker, _ := dlock.New(cfg,
    dlock.WithRedisConnector(redisConn),
    dlock.WithLogger(logger))

// 自动添加的日志字段
// {
//   "component": "dlock",
//   "backend": "redis",
//   "key": "resource-key",
//   ...
// }
```

### 指标示例（预留）

```go
locker, _ := dlock.New(cfg,
    dlock.WithRedisConnector(redisConn),
    dlock.WithLogger(logger),
    dlock.WithMeter(meter))

// 指标名称已预留，后续将接入采集：
// - dlock_lock_acquired_total: 锁获取成功计数
// - dlock_lock_failed_total: 锁获取失败计数
// - dlock_lock_released_total: 锁释放计数
// - dlock_lock_hold_duration_seconds: 锁持有时长
```

## 工厂函数

### New

```go
func New(cfg *Config, opts ...Option) (Locker, error)
```

根据 `cfg.Driver` 创建分布式锁实例。

**参数**：

- `cfg`: 组件配置
- `opts`: 可选参数（`WithRedisConnector`、`WithEtcdConnector`、`WithLogger`、`WithMeter`）

**示例**：

```go
locker, err := dlock.New(&dlock.Config{
    Driver:        dlock.DriverRedis,
    Prefix:        "app:lock:",
    DefaultTTL:    15 * time.Second,
    RetryInterval: 100 * time.Millisecond,
}, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))
```

## 标准错误

dlock 定义了以下标准错误，使用 `xerrors` 进行包装：

```go
var (
    ErrConfigNil       = xerrors.New("dlock: config is nil")
    ErrConnectorNil    = xerrors.New("dlock: connector is nil")
    ErrLockNotHeld     = xerrors.New("dlock: lock not held")
    ErrLockAlreadyHeld = xerrors.New("dlock: lock already held locally")
    ErrOwnershipLost   = xerrors.New("dlock: ownership lost")
)
```

**示例**：

```go
if err := locker.Unlock(ctx, key); err != nil {
    if xerrors.Is(err, dlock.ErrLockNotHeld) {
        logger.Warn("lock not held, ignoring", clog.String("key", key))
        return nil
    }
    return xerrors.Wrapf(err, "failed to unlock key: %s", key)
}
```

## 指标定义（预留）

当前仅定义指标名称，采集尚未启用（即使注入 `Meter` 也不会上报）：

| 指标名                             | 类型      | 说明           | 标签             |
| ---------------------------------- | --------- | -------------- | ---------------- |
| `dlock_lock_acquired_total`        | Counter   | 锁获取成功次数 | `backend`, `key` |
| `dlock_lock_failed_total`          | Counter   | 锁获取失败次数 | `backend`, `key` |
| `dlock_lock_released_total`        | Counter   | 锁释放次数     | `backend`, `key` |
| `dlock_lock_hold_duration_seconds` | Histogram | 锁持有时长     | `backend`, `key` |

## 完整示例

参考 [examples/dlock/main.go](../examples/dlock/main.go) 了解更多使用示例。

## 设计原则

- **接口抽象**：业务代码仅依赖 `Locker` 接口，实现细节隐藏在 `internal/dlock/` 中
- **后端无关**：支持多种后端存储，通过配置灵活切换
- **安全性**：通过令牌机制和自动续期确保锁的安全性
- **依赖注入**：使用显式的函数式选项模式，不维护全局状态
- **可观测性**：通过可选注入 Logger 实现统一日志，Meter 为预留扩展点

## 相关文档

- [dlock 设计文档](../docs/business/dlock-design.md)
- [Genesis 架构设计](../docs/genesis-design.md)
- [Connector 层](../connector/README.md)
