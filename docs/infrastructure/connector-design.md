# 连接器设计文档 (Connector Design)

## 1. 概述

连接器 (Connector) 是 Genesis 基础设施层的核心组件，负责管理与外部服务（MySQL、Redis、Etcd、NATS）的原始连接。

**核心职责**：

- 创建和管理连接池
- 提供类型安全的客户端访问
- 健康检查与连接监控
- 资源释放

**设计原则**：

- **资源所有权**：Connector 拥有连接，必须调用 `Close()` 释放
- **最小接口**：只暴露必要的方法，避免过度设计
- **扁平化结构**：接口、配置、实现放在同一包下
- **统一 L0 组件**：使用 Genesis 的 `clog`、`metrics`、`xerrors` 实现日志、监控、错误处理

## 2. 接口设计

### 2.1. 核心接口

```go
// pkg/connector/interface.go

// Connector 基础连接器接口
type Connector interface {
    // Connect 建立连接，应幂等且并发安全
    Connect(ctx context.Context) error
    // Close 关闭连接，释放资源
    Close() error
    // HealthCheck 检查连接健康状态
    HealthCheck(ctx context.Context) error
    // IsHealthy 返回缓存的健康状态标志
    IsHealthy() bool
    // Name 返回连接实例名称
    Name() string
}

// TypedConnector 泛型接口，提供类型安全的客户端访问
type TypedConnector[T any] interface {
    Connector
    GetClient() T
}
```

### 2.2. 具体类型接口

```go
// RedisConnector Redis 连接器接口
type RedisConnector interface {
    TypedConnector[*redis.Client]
}

// MySQLConnector MySQL 连接器接口
type MySQLConnector interface {
    TypedConnector[*gorm.DB]
}

// EtcdConnector Etcd 连接器接口
type EtcdConnector interface {
    TypedConnector[*clientv3.Client]
}

// NATSConnector NATS 连接器接口
type NATSConnector interface {
    TypedConnector[*nats.Conn]
}
```

### 2.3. 设计说明

**为什么移除 Lifecycle 接口？**

原设计中 Connector 继承了 `Lifecycle` 接口（`Start/Stop/Phase`），这是为 DI 容器服务的。现在采用 Go Native 依赖注入后：

| 原方法 | 问题 | 解决方案 |
|--------|------|----------|
| `Start()` | 与 `Connect()` 重复 | 删除，使用 `Connect()` |
| `Stop()` | 与 `Close()` 重复 | 删除，使用 `Close()` |
| `Phase()` | 只为 Container 服务 | 删除，使用 defer 自然排序 |

## 3. 配置结构

### 3.1. 通用配置

```go
// pkg/connector/config.go

// BaseConfig 通用连接配置
type BaseConfig struct {
    Name            string        `mapstructure:"name"`
    MaxRetries      int           `mapstructure:"max_retries"`
    RetryInterval   time.Duration `mapstructure:"retry_interval"`
    ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`
    HealthCheckFreq time.Duration `mapstructure:"health_check_freq"`
}

func (c *BaseConfig) SetDefaults() {
    if c.Name == "" {
        c.Name = "default"
    }
    if c.MaxRetries == 0 {
        c.MaxRetries = 3
    }
    if c.RetryInterval == 0 {
        c.RetryInterval = time.Second
    }
    if c.ConnectTimeout == 0 {
        c.ConnectTimeout = 5 * time.Second
    }
}
```

### 3.2. 具体连接器配置

```go
// RedisConfig Redis 连接配置
type RedisConfig struct {
    BaseConfig   `mapstructure:",squash"`
    Addr         string `mapstructure:"addr"`
    Password     string `mapstructure:"password"`
    DB           int    `mapstructure:"db"`
    PoolSize     int    `mapstructure:"pool_size"`
    MinIdleConns int    `mapstructure:"min_idle_conns"`
}

// MySQLConfig MySQL 连接配置
type MySQLConfig struct {
    BaseConfig      `mapstructure:",squash"`
    DSN             string `mapstructure:"dsn"`
    MaxOpenConns    int    `mapstructure:"max_open_conns"`
    MaxIdleConns    int    `mapstructure:"max_idle_conns"`
    ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

// EtcdConfig Etcd 连接配置
type EtcdConfig struct {
    BaseConfig   `mapstructure:",squash"`
    Endpoints    []string `mapstructure:"endpoints"`
    DialTimeout  time.Duration `mapstructure:"dial_timeout"`
    Username     string `mapstructure:"username"`
    Password     string `mapstructure:"password"`
}

// NATSConfig NATS 连接配置
type NATSConfig struct {
    BaseConfig      `mapstructure:",squash"`
    URL             string `mapstructure:"url"`
    MaxReconnects   int    `mapstructure:"max_reconnects"`
    ReconnectWait   time.Duration `mapstructure:"reconnect_wait"`
}
```

## 4. 错误处理

使用 Genesis 的 `xerrors` 组件定义和包装错误：

```go
// pkg/connector/errors.go
import "github.com/ceyewan/genesis/pkg/xerrors"

// Sentinel Errors
var (
    ErrNotConnected  = xerrors.New("connector: not connected")
    ErrAlreadyClosed = xerrors.New("connector: already closed")
    ErrConnection    = xerrors.New("connector: connection failed")
    ErrTimeout       = xerrors.New("connector: timeout")
    ErrConfig        = xerrors.New("connector: invalid config")
    ErrHealthCheck   = xerrors.New("connector: health check failed")
)

// 使用时包装错误
func (c *redisConnector) Connect(ctx context.Context) error {
    if err := c.client.Ping(ctx).Err(); err != nil {
        return xerrors.Wrapf(ErrConnection, "redis %s: %v", c.cfg.Addr, err)
    }
    return nil
}
```

## 5. 工厂函数

### 5.1. 标准签名

```go
// 工厂函数签名
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error)
func NewMySQL(cfg *MySQLConfig, opts ...Option) (MySQLConnector, error)
func NewEtcd(cfg *EtcdConfig, opts ...Option) (EtcdConnector, error)
func NewNATS(cfg *NATSConfig, opts ...Option) (NATSConnector, error)

// Must 版本（panic on error）
func MustNewRedis(cfg *RedisConfig, opts ...Option) RedisConnector
func MustNewMySQL(cfg *MySQLConfig, opts ...Option) MySQLConnector
```

### 5.2. Option 模式

使用 Genesis 的 `clog` 和 `metrics` 组件：

```go
// pkg/connector/options.go
import (
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/metrics"
)

type options struct {
    logger clog.Logger
    meter  metrics.Meter
}

type Option func(*options)

func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.WithNamespace("connector")
    }
}

func WithMeter(m metrics.Meter) Option {
    return func(o *options) {
        o.meter = m
    }
}
```

## 6. 实现示例

### 6.1. Redis 连接器

```go
// pkg/connector/redis.go

type redisConnector struct {
    cfg     *RedisConfig
    client  *redis.Client
    logger  clog.Logger
    healthy atomic.Bool
    mu      sync.RWMutex
}

func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
    cfg.SetDefaults()
    if err := cfg.Validate(); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }

    opt := &options{}
    for _, o := range opts {
        o(opt)
    }

    c := &redisConnector{
        cfg:    cfg,
        logger: opt.logger.With("connector", "redis", "name", cfg.Name),
    }

    // 创建 Redis 客户端
    c.client = redis.NewClient(&redis.Options{
        Addr:         cfg.Addr,
        Password:     cfg.Password,
        DB:           cfg.DB,
        PoolSize:     cfg.PoolSize,
        MinIdleConns: cfg.MinIdleConns,
    })

    return c, nil
}

func (c *redisConnector) Connect(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    if err := c.client.Ping(ctx).Err(); err != nil {
        return &Error{
            Type:      ErrConnection,
            Connector: c.cfg.Name,
            Cause:     err,
            Retryable: true,
        }
    }

    c.healthy.Store(true)
    c.logger.Info("connected to redis", "addr", c.cfg.Addr)
    return nil
}

func (c *redisConnector) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.healthy.Store(false)
    if c.client != nil {
        return c.client.Close()
    }
    return nil
}

func (c *redisConnector) HealthCheck(ctx context.Context) error {
    if err := c.client.Ping(ctx).Err(); err != nil {
        c.healthy.Store(false)
        return err
    }
    c.healthy.Store(true)
    return nil
}

func (c *redisConnector) IsHealthy() bool {
    return c.healthy.Load()
}

func (c *redisConnector) Name() string {
    return c.cfg.Name
}

func (c *redisConnector) GetClient() *redis.Client {
    return c.client
}
```

## 7. 目录结构

采用扁平化结构，所有内容放在 `pkg/connector/` 下：

```text
pkg/connector/
├── interface.go    # Connector 接口定义
├── config.go       # 配置结构体
├── errors.go       # 错误定义
├── options.go      # Option 函数
├── redis.go        # Redis 实现
├── mysql.go        # MySQL 实现
├── etcd.go         # Etcd 实现
└── nats.go         # NATS 实现
```

**说明**：

- 不再有 `internal/connector/` 目录
- 不再有 `types/` 子包
- 实现类型使用非导出结构体（如 `redisConnector`）

## 8. 使用示例

### 8.1. 基本使用

```go
func main() {
    ctx := context.Background()

    // 创建 Logger
    logger, _ := clog.New(&clog.Config{Level: "info"})

    // 创建 Redis 连接器
    redisConn, err := connector.NewRedis(&connector.RedisConfig{
        Addr:     "localhost:6379",
        PoolSize: 10,
    }, connector.WithLogger(logger))
    if err != nil {
        log.Fatal(err)
    }
    defer redisConn.Close() // 确保资源释放

    // 建立连接
    if err := redisConn.Connect(ctx); err != nil {
        log.Fatal(err)
    }

    // 使用客户端
    client := redisConn.GetClient()
    client.Set(ctx, "key", "value", time.Minute)
}
```

### 8.2. 与组件配合使用

```go
func main() {
    ctx := context.Background()

    // 1. 创建连接器
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()
    redisConn.Connect(ctx)

    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
    defer mysqlConn.Close()
    mysqlConn.Connect(ctx)

    // 2. 创建组件（注入连接器）
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock)
    // locker.Close() 是 no-op，无需 defer

    database, _ := db.New(mysqlConn, &cfg.DB)
    // database.Close() 是 no-op，无需 defer

    // 3. 业务逻辑...
}
```

### 8.3. 健康检查

```go
// 定期健康检查
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        if err := redisConn.HealthCheck(ctx); err != nil {
            logger.Warn("redis health check failed", "error", err)
        }
    }
}()

// 快速状态查询
if !redisConn.IsHealthy() {
    // 处理不健康状态
}
```

## 9. 资源所有权

### 9.1. 核心原则

**谁创建，谁负责关闭。**

```text
┌─────────────────────────────────────────────────────────────────┐
│                     资源所有权模型                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   Connector (Owner)                                             │
│   ├── 创建 *redis.Client / *gorm.DB                            │
│   ├── 管理连接池                                                │
│   └── Close() 释放资源  ←── 必须调用                            │
│                                                                 │
│   Component (Borrower)                                          │
│   ├── Cache    ─┐                                               │
│   ├── DLock    ─┼── 借用 Connector.GetClient()                  │
│   ├── DB       ─┤                                               │
│   └── MQ       ─┘                                               │
│        │                                                        │
│        └── Close() 是 no-op（不拥有资源）                        │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

### 9.2. 生命周期管理

使用 Go 的 `defer` 自然管理资源，无需 Lifecycle 接口：

```go
func main() {
    // 创建顺序：Redis -> MySQL -> 组件
    // 关闭顺序：组件 -> MySQL -> Redis (defer 自动逆序)
    
    redisConn, _ := connector.NewRedis(&cfg.Redis)
    defer redisConn.Close() // 最后关闭
    
    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL)
    defer mysqlConn.Close() // 倒数第二关闭
    
    // 组件借用连接器，无需 Close
    cache, _ := cache.New(redisConn, &cfg.Cache)
    db, _ := db.New(mysqlConn, &cfg.DB)
}
```

## 10. 设计决策

### 10.1. 为什么不用 Manager 模式？

原设计有 `internal/connector/manager` 用于连接复用和引用计数。现在简化为：

| 原 Manager 功能 | 现在的处理方式 |
|----------------|---------------|
| 实例缓存 | 用户在 main.go 中管理实例 |
| 引用计数 | 使用 defer 确保 Close |
| 并发安全 | 每个 Connector 内部处理 |

**优点**：

- 简化代码，减少抽象层次
- 用户对资源有完全控制权
- 符合 Go 显式优于隐式的原则

### 10.2. 为什么扁平化结构？

| 原结构 | 问题 |
|--------|------|
| `pkg/connector/types/` | 导入路径冗长 |
| `internal/connector/` | 实现分散，难以理解 |

**现在**：所有内容在 `pkg/connector/` 下，用户只需一个导入：

```go
import "github.com/ceyewan/genesis/pkg/connector"

conn, _ := connector.NewRedis(&connector.RedisConfig{...})
```

### 10.3. Connect vs NewWithConnect

两种模式都支持：

```go
// 模式 1：分步（推荐，可处理连接错误）
conn, err := connector.NewRedis(&cfg)
if err != nil { /* 配置错误 */ }
if err := conn.Connect(ctx); err != nil { /* 连接错误 */ }

// 模式 2：一步到位
conn, err := connector.NewRedisAndConnect(ctx, &cfg)
```

## 11. 可观测性

### 11.1. 日志

每个连接器接收 Logger 并派生命名空间：

```go
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
    // ...
    c.logger = opt.logger.With("connector", "redis", "name", cfg.Name)
    // 输出: {"connector":"redis", "name":"primary", "msg":"connected"}
}
```

### 11.2. 指标

可选注入 Meter 收集连接池指标：

```go
redisConn, _ := connector.NewRedis(&cfg.Redis,
    connector.WithLogger(logger),
    connector.WithMeter(tel.Meter()),
)
```

常用指标：

- `connector_pool_active` - 活跃连接数
- `connector_pool_idle` - 空闲连接数
- `connector_errors_total` - 错误计数
- `connector_latency_seconds` - 操作延迟

### 11.3. L0 组件集成总结

Connector 统一使用 Genesis L0 组件：

| 能力 | L0 组件 | Option |
|------|---------|--------|
| 日志 | `clog` | `WithLogger(logger)` |
| 指标 | `metrics` | `WithMeter(meter)` |
| 错误 | `xerrors` | 使用 Sentinel Errors 和 `xerrors.Wrap` |
| 配置 | `config` | 配置结构定义在 `connector.RedisConfig` 等 |
