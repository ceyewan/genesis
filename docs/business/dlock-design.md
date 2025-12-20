# dlock 分布式锁组件设计文档

## 1. 目标与原则

`dlock` (Distributed Lock) 旨在为微服务框架提供一个统一、安全且易于使用的分布式锁组件。它屏蔽了底层存储（Redis, Etcd 等）的差异，提供一致的 API 体验。

**设计原则：**

1. **接口抽象 (Abstraction):** 业务代码只依赖 `dlock.Locker` 接口，不感知底层实现。
2. **后端无关 (Backend Agnostic):** 支持多种后端（Redis, Etcd），并通过配置切换。
3. **安全性 (Safety):** 默认支持自动续期 (Watchdog/KeepAlive) 和防误删机制。
4. **依赖注入 (Dependency Injection):** 不自行管理连接，而是依赖 `connector` 层提供的连接实例。
5. **统一配置 (Unified Config):** 使用结构化的配置对象进行初始化。
6. **可观测性 (Observability):** 支持注入 Logger, Meter, Tracer，实现统一的可观测性。
7. **双模式支持 (Dual Mode):** 支持独立模式和容器模式两种使用方式。

## 2. 项目结构

遵循框架整体的分层设计，API 与实现分离：

```text
genesis/
├── pkg/
│   └── dlock/                  # 公开 API 入口
│       ├── dlock.go            # 工厂函数 (NewRedis, NewEtcd) + 类型导出
│       ├── options.go          # 组件初始化 Option (WithLogger, WithMeter, WithTracer)
│       └── types/              # 类型定义
│           ├── interface.go    # Locker 接口
│           ├── config.go       # 配置定义
│           └── options.go      # Lock 操作 Option (WithTTL)
├── internal/
│   └── dlock/                  # 内部实现
│       ├── factory.go          # 实现工厂逻辑
│       ├── redis/              # Redis 实现
│       │   └── locker.go
│       └── etcd/               # Etcd 实现
│           └── locker.go
└── ...
```

**注意区分两种 Option**:

- **组件初始化 Option** (`pkg/dlock/options.go`): 用于创建 Locker 实例时注入依赖 (Logger, Meter, Tracer)
- **Lock 操作 Option** (`pkg/dlock/types/options.go`): 用于 Lock/TryLock 调用时设置运行时参数 (TTL)

## 3. 核心 API 设计

核心定义位于 `pkg/dlock/types/`。

### 3.1. Locker 接口

```go
// pkg/dlock/types/interface.go

package types

import (
    "context"
    "time"
)

// Locker 定义了分布式锁的核心行为
type Locker interface {
    // Lock 阻塞式加锁
    // 成功返回 nil，失败返回错误
    // 如果上下文取消，返回 context.Canceled 或 context.DeadlineExceeded
    Lock(ctx context.Context, key string, opts ...Option) error

    // TryLock 非阻塞式尝试加锁
    // 成功获取锁返回 true, nil
    // 锁已被占用返回 false, nil
    // 发生错误返回 false, err
    TryLock(ctx context.Context, key string, opts ...Option) (bool, error)

    // Unlock 释放锁
    // 只有锁的持有者才能成功释放
    Unlock(ctx context.Context, key string) error
}
```

### 3.2. 配置 (Config)

```go
// pkg/dlock/types/config.go

package types

// BackendType 定义支持的后端类型
type BackendType string

const (
    BackendRedis BackendType = "redis"
    BackendEtcd  BackendType = "etcd"
)

// Config 组件静态配置
type Config struct {
    // Backend 选择使用的后端 (redis | etcd)
    Backend BackendType `json:"backend" yaml:"backend"`

    // Prefix 锁 Key 的全局前缀，例如 "dlock:"
    Prefix string `json:"prefix" yaml:"prefix"`

    // DefaultTTL 默认锁超时时间
    DefaultTTL time.Duration `json:"default_ttl" yaml:"default_ttl"`

    // RetryInterval 加锁重试间隔 (仅 Lock 模式有效)
    RetryInterval time.Duration `json:"retry_interval" yaml:"retry_interval"`
}
```

### 3.3. Lock 操作选项 (Lock Option)

支持在调用 `Lock`/`TryLock` 时覆盖默认配置。

```go
// pkg/dlock/types/options.go

package types

import "time"

// LockOptions Lock 操作的选项配置
type LockOptions struct {
    TTL time.Duration
}

// LockOption Lock 操作的选项函数
type LockOption func(*LockOptions)

// WithTTL 设置锁的 TTL（超时时间）
func WithTTL(d time.Duration) LockOption {
    return func(o *LockOptions) {
        o.TTL = d
    }
}
```

### 3.4. 组件初始化选项 (Component Option)

支持在创建 Locker 实例时注入依赖。

```go
// pkg/dlock/options.go

package dlock

import (
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Option 组件初始化选项函数
type Option func(*Options)

// Options 选项结构
type Options struct {
    Logger clog.Logger
    Meter  types.Meter
    Tracer types.Tracer
}

// WithLogger 注入日志记录器
func WithLogger(l clog.Logger) Option {
    return func(o *Options) {
        if l != nil {
            o.Logger = l.WithNamespace("dlock")
        }
    }
}

// WithMeter 注入指标 Meter
func WithMeter(m types.Meter) Option {
    return func(o *Options) {
        o.Meter = m
    }
}

// WithTracer 注入 Tracer
func WithTracer(t types.Tracer) Option {
    return func(o *Options) {
        o.Tracer = t
    }
}
```

## 4. 工厂函数设计

### 4.1. 独立模式工厂函数

提供 `NewRedis` 和 `NewEtcd` 两个工厂函数，支持独立模式使用。

```go
// pkg/dlock/dlock.go

// NewRedis 创建 Redis 分布式锁 (独立模式)
func NewRedis(conn connector.RedisConnector, cfg *Config, opts ...Option) (Locker, error) {
    // 应用选项
    opt := Options{
        Logger: clog.Default(), // 默认 Logger
    }
    for _, o := range opts {
        o(&opt)
    }

    return internaldlock.NewRedis(conn, cfg, opt.Logger, opt.Meter, opt.Tracer)
}

// NewEtcd 创建 Etcd 分布式锁 (独立模式)
func NewEtcd(conn connector.EtcdConnector, cfg *Config, opts ...Option) (Locker, error) {
    // 应用选项
    opt := Options{
        Logger: clog.Default(), // 默认 Logger
    }
    for _, o := range opts {
        o(&opt)
    }

    return internaldlock.NewEtcd(conn, cfg, opt.Logger, opt.Meter, opt.Tracer)
}
```

### 4.2. 容器模式集成

在 Container 中根据配置的 Backend 自动选择后端。

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

## 5. 内部实现设计

### 5.1. 依赖注入与可观测性集成

`dlock` 不负责创建 Redis 或 Etcd 的连接，而是通过构造函数接收 `connector` 接口。同时接收 Logger, Meter, Tracer 用于可观测性。

```go
// internal/dlock/redis/locker.go

type RedisLocker struct {
    client *redis.Client // 来自 pkg/connector.RedisConnector
    cfg    *types.Config
    logger clog.Logger   // 注入的 Logger，自动带有 "dlock" namespace
    meter  telemetrytypes.Meter
    tracer telemetrytypes.Tracer
    // ...
}

// New 创建 RedisLocker 实例
func New(
    conn connector.RedisConnector,
    cfg *types.Config,
    logger clog.Logger,
    meter telemetrytypes.Meter,
    tracer telemetrytypes.Tracer,
) (*RedisLocker, error) {
    // ...
}
```

### 5.2. 自动续期 (Watchdog)

- **Redis:** 启动后台 Goroutine，定期（如 TTL/3）检查并刷新 Key 的过期时间。解锁时通过 Channel 通知停止。
- **Etcd:** 使用 `clientv3.Lease` 的 `KeepAlive` 机制。

### 5.3. 防误删

- **Redis:** 使用 Lua 脚本，校验 Value（Token）一致后再删除。
- **Etcd:** 依赖 Lease ID 或 Revision 机制。

## 6. 使用示例

### 6.1. 独立模式

```go
package main

import (
    "context"
    "time"

    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/dlock"
)

func main() {
    // 创建 Logger
    logger := clog.New(&clog.Config{
        Level: "info",
        Format: "json",
    })

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
    key := "order:123"

    // 阻塞加锁，自定义 TTL
    if err := locker.Lock(ctx, key, dlock.WithTTL(10*time.Second)); err != nil {
        logger.Error("failed to lock", clog.Error(err))
        return
    }
    defer locker.Unlock(ctx, key)

    // 业务逻辑
    processOrder()
}
```

### 6.2. 容器模式

```go
package main

import (
    "context"
    "time"

    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/container"
    "github.com/ceyewan/genesis/pkg/dlock"
)

func main() {
    // 创建 Logger
    logger := clog.New(&clog.Config{
        Level: "info",
        Format: "json",
    })

    // 创建 Container
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

    // 使用锁
    ctx := context.Background()
    key := "order:123"

    // 阻塞加锁
    // 日志输出: level=info msg="lock acquired" namespace=app.dlock key=order:123
    if err := app.DLock.Lock(ctx, key); err != nil {
        app.Log.Error("failed to lock", clog.Error(err))
        return
    }
    defer app.DLock.Unlock(ctx, key)

    // 业务逻辑
    processOrder()
}
```

### 6.3. 非阻塞加锁

```go
// 尝试加锁，不阻塞
locked, err := app.DLock.TryLock(ctx, "resource:456")
if err != nil {
    app.Log.Error("failed to try lock", clog.Error(err))
    return
}
if !locked {
    app.Log.Warn("resource is locked by another process")
    return
}
defer app.DLock.Unlock(ctx, "resource:456")

// 业务逻辑
processResource()
```
