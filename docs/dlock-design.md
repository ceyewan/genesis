# dlock 分布式锁组件设计文档

## 1. 目标与原则

`dlock` (Distributed Lock) 旨在为微服务框架提供一个统一、安全且易于使用的分布式锁组件。它屏蔽了底层存储（Redis, Etcd 等）的差异，提供一致的 API 体验。

**设计原则：**

1. **接口抽象 (Abstraction):** 业务代码只依赖 `dlock.Locker` 接口，不感知底层实现。
2. **后端无关 (Backend Agnostic):** 支持多种后端（Redis, Etcd），并通过配置切换。
3. **安全性 (Safety):** 默认支持自动续期 (Watchdog/KeepAlive) 和防误删机制。
4. **依赖注入 (Dependency Injection):** 不自行管理连接，而是依赖 `connector` 层提供的连接实例。
5. **统一配置 (Unified Config):** 使用结构化的配置对象进行初始化。
6. **可观测性 (Observability):** 集成 `clog`，通过注入 Logger 实现统一的日志规范（Namespace）。

## 2. 项目结构

遵循框架整体的分层设计，API 与实现分离：

```text
genesis/
├── pkg/
│   └── dlock/                  # 公开 API 入口
│       ├── dlock.go            # 工厂函数 (New)
│       └── types/              # 类型定义
│           ├── interface.go    # Locker 接口
│           ├── config.go       # 配置定义
│           └── options.go      # 运行时选项
├── internal/
│   └── dlock/                  # 内部实现
│       ├── factory.go          # 实现工厂逻辑
│       ├── redis/              # Redis 实现
│       │   └── locker.go
│       └── etcd/               # Etcd 实现
│           └── locker.go
└── ...
```

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

### 3.3. 选项 (Option)

支持在调用 `Lock`/`TryLock` 时覆盖默认配置。

```go
// pkg/dlock/types/options.go

package types

type LockOption struct {
    TTL time.Duration
}

type Option func(*LockOption)

func WithTTL(d time.Duration) Option {
    return func(o *LockOption) {
        o.TTL = d
    }
}
```

## 4. 内部实现设计

### 4.1. 依赖注入与日志集成

`dlock` 不负责创建 Redis 或 Etcd 的连接，而是通过构造函数接收 `connector` 接口。同时接收 `clog.Logger` 用于日志记录。

```go
// internal/dlock/redis/locker.go

type RedisLocker struct {
    client *redis.Client // 来自 pkg/connector.RedisConnector
    cfg    *types.Config
    logger clog.Logger   // 注入的 Logger，通常带有 "dlock" namespace
    // ...
}

// New 创建 RedisLocker 实例
// logger: 建议传入 namespace="dlock" 的 logger
func New(conn connector.RedisConnector, cfg *types.Config, logger clog.Logger) (*RedisLocker, error) {
    // ...
}
```

### 4.2. 自动续期 (Watchdog)

- **Redis:** 启动后台 Goroutine，定期（如 TTL/3）检查并刷新 Key 的过期时间。解锁时通过 Channel 通知停止。
- **Etcd:** 使用 `clientv3.Lease` 的 `KeepAlive` 机制。

### 4.3. 防误删

- **Redis:** 使用 Lua 脚本，校验 Value（Token）一致后再删除。
- **Etcd:** 依赖 Lease ID 或 Revision 机制。

## 5. 容器集成 (Container Integration)

在 `pkg/container` 中集成 `dlock`，负责 Logger 的 Namespace 派生。

```go
// pkg/container/container.go

type Container struct {
    // ...
    DLock dlock.Locker
    // ...
}

func (c *Container) initDLock(cfg *Config) error {
    if cfg.DLock == nil {
        return nil
    }

    // 派生 dlock 专用的 Logger
    // 假设 c.Log 的 namespace 是 "user-service"
    // dlockLogger 的 namespace 将是 "user-service.dlock"
    dlockLogger := c.Log.WithNamespace("dlock")

    switch cfg.DLock.Backend {
    case types.BackendRedis:
        // 获取 Redis 连接器 (其内部 Logger namespace 可能是 "user-service.redis")
        redisConn, err := c.GetRedisConnector(*cfg.Redis)
        // 创建 dlock
        c.DLock = dlock.NewRedis(redisConn, cfg.DLock, dlockLogger)
    case types.BackendEtcd:
        // 获取 Etcd 连接器
        etcdConn, err := c.GetEtcdConnector(*cfg.Etcd)
        // 创建 dlock
        c.DLock = dlock.NewEtcd(etcdConn, cfg.DLock, dlockLogger)
    }
    return nil
}
```

## 6. 使用示例

```go
func main() {
    // ... 容器初始化 ...
    
    // 使用分布式锁
    ctx := context.Background()
    key := "resource:123"
    
    // 1. 阻塞加锁
    // 日志输出示例: level=info msg="lock acquired" namespace=user-service.dlock key=resource:123
    if err := app.DLock.Lock(ctx, key); err != nil {
        app.Log.Error("failed to lock", clog.Error(err))
        return
    }
    defer app.DLock.Unlock(ctx, key)
    
    // 2. 业务逻辑
    processResource()
}
