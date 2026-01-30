# Genesis connector：微服务外部连接管理的设计与实现

Genesis `connector` 是基础设施层（L1）的核心组件，负责管理与外部服务（MySQL、PostgreSQL、SQLite、Redis、Etcd、NATS、Kafka）的原始连接。它通过封装复杂的连接细节、提供健康检查、生命周期管理以及与 L0 组件（日志、错误）的深度集成，为上层组件提供稳定、类型安全的连接能力。

---

## 0. 摘要

- `connector` 把"外部连接"建模为**连接器（Connector）接口**，隐藏底层客户端的实现差异，提供统一的初始化、连接、健康检查与关闭语义。
- 工程实践中常见的说法"谁创建谁释放"，本质是：**连接器拥有底层连接的生命周期所有权，上层组件仅借用客户端**，避免资源泄露与双重关闭问题。
- 连接器的**两阶段初始化**（New → Connect）让配置验证与 I/O 操作分离，在应用启动阶段实现 Fail-fast，同时在系统就绪后再建立实际连接。
- 健康检查的**主动探测（HealthCheck）与状态缓存（IsHealthy）分离**，避免高频率健康检查对下游服务造成压力，同时支持调用方的快速状态查询。
- 泛型接口 `TypedConnector[T]` 提供类型安全的客户端访问（如 `*redis.Client`、`*gorm.DB`），避免运行时类型断言。

---

## 1. 背景：微服务外部连接要解决的"真实问题"

在微服务场景中，一个服务通常需要与多个外部系统交互：

- **存储层**：MySQL、PostgreSQL、Redis、MongoDB
- **协调层**：Etcd、Consul
- **消息层**：Kafka、NATS、RabbitMQ
- **缓存层**：Redis、Memcached

这些系统的客户端库差异巨大：

| 系统   | 客户端库          | 初始化方式               | 连接验证方式 |
|:------:|------------------|------------------------|--------------|
| Redis  | `go-redis/redis` | `NewClient()`          | `Ping()`     |
| MySQL  | `GORM`           | `Open()` + `Ping()`    | `SQLOpen().Ping()` |
| Etcd   | `etcd/clientv3`  | `New()`                | 无需 Ping（连接时阻塞） |
| Kafka  | `franz-go`       | `NewClient()`          | `Ping()`     |

如果直接在业务代码中使用这些客户端，会面临以下问题：

- **初始化模式不统一**：有的需要显式 Connect，有的在 New 时就建立连接。
- **健康检查方式各异**：有的提供 Ping，有的需要发送测试请求。
- **资源管理复杂**：连接池超时、空闲连接清理、优雅关闭等逻辑分散。
- **错误处理不一致**：不同客户端的错误类型不同，难以统一处理。
- **可观测性缺失**：缺少统一的日志命名空间、指标埋点、链路追踪。

结论是：需要一个统一的抽象层来管理外部连接，封装差异，提供一致的语义——这正是 `connector` 组件要做的事。

---

## 2. 核心设计：两阶段初始化与资源所有权

### 2.1 两阶段初始化：New 与 Connect 分离

`connector` 的所有连接器都遵循两阶段初始化模式：

```go
// 阶段 1：创建连接器实例（仅验证配置，不执行 I/O）
conn, err := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
})
if err != nil {
    // 配置错误，立即失败
    panic(err)
}

// 阶段 2：建立实际连接（执行 Ping/Auth 等 I/O 操作）
if err := conn.Connect(ctx); err != nil {
    // 连接失败
    panic(err)
}
```

这种设计的收益：

- **Fail-fast**：配置错误在启动阶段就能发现，而不是运行时。
- **灵活的连接时机**：可以在应用完全就绪后再连接（如等待依赖服务启动）。
- **幂等性**：多次调用 Connect 是安全的，便于重试逻辑。

### 2.2 资源所有权：谁创建，谁释放

`connector` 采用**借用模型（Borrowing Model）**：

```
┌─────────────────────────────────────────────────────────────┐
│  应用层 (main.go)                                            │
│                                                             │
│  redisConn := connector.NewRedis(...)  ──┐                 │
│  defer redisConn.Close()                  │ 拥有者          │
│                                          │ (Owner)         │
│  cache := cache.New(redisConn, ...)    ◄─┘                 │
│  // cache 仅借用 redisConn，不负责关闭   │ 借用者          │
│                                          │ (Borrower)      │
└─────────────────────────────────────────────────────────────┘
```

所有权规则：

1. **连接器（Owner）**：拥有底层连接，负责 Close()，生命周期由应用层通过 defer 管理。
2. **上层组件（Borrower）**：如 `cache`、`db`、`mq` 等，它们借用连接器中的客户端，Close() 通常是 no-op。
3. **LIFO 关闭顺序**：使用 defer 确保关闭顺序与创建顺序相反。

---

## 3. 接口设计：统一抽象与类型安全

### 3.1 基础连接器接口

所有连接器都实现 `Connector` 基础接口：

```go
type Connector interface {
    // Connect 建立连接（幂等，多次调用安全）
    Connect(ctx context.Context) error

    // Close 关闭连接（幂等，多次调用安全）
    Close() error

    // HealthCheck 主动健康检查
    HealthCheck(ctx context.Context) error

    // IsHealthy 返回缓存的健康状态（无 I/O）
    IsHealthy() bool

    // Name 返回连接器名称
    Name() string
}
```

### 3.2 类型化连接器接口

为了提供类型安全的客户端访问，引入泛型接口：

```go
type TypedConnector[T any] interface {
    Connector
    GetClient() T
}
```

使用示例：

```go
// 类型明确，无需类型断言
var client *redis.Client = conn.GetClient()
client.Set(ctx, "key", "value", 0)
```

### 3.3 专用接口

每种连接器还提供专用接口，便于在函数签名中明确依赖：

```go
type RedisConnector interface {
    TypedConnector[*redis.Client]
}

type MySQLConnector interface {
    TypedConnector[*gorm.DB]
}

type PostgreSQLConnector interface {
    TypedConnector[*gorm.DB]
}
```

---

## 4. 配置设计：扁平化与默认值

### 4.1 配置结构原则

所有配置结构遵循以下原则：

- **扁平化**：不使用嵌套的子配置，所有字段平铺。
- **核心参数必填**：Host/Port/Addr 等必填字段 validate 时检查。
- **可选参数有默认值**：通过 setDefaults() 方法自动填充。
- **支持 DSN 透传**：对于数据库类连接器，支持直接传入完整 DSN。

示例（MySQL）：

```go
type MySQLConfig struct {
    // 基础配置
    Name string `mapstructure:"name"` // 连接器名称 (默认: "default")

    // 核心配置
    DSN      string `mapstructure:"dsn"`      // 完整 DSN（可选，优先级最高）
    Host     string `mapstructure:"host"`     // 主机地址（DSN 未设置时必填）
    Port     int    `mapstructure:"port"`     // 端口（默认: 3306）
    Username string `mapstructure:"username"` // 用户名（DSN 未设置时必填）
    Password string `mapstructure:"password"` // 密码
    Database string `mapstructure:"database"` // 数据库名（DSN 未设置时必填）

    // 高级配置（有默认值）
    Charset         string        `mapstructure:"charset"`           // 默认: "utf8mb4"
    MaxIdleConns    int           `mapstructure:"max_idle_conns"`    // 默认: 10
    MaxOpenConns    int           `mapstructure:"max_open_conns"`    // 默认: 100
    ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"` // 默认: 1h
    ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 默认: 5s
}
```

### 4.2 配置处理流程

配置处理在 New* 函数内部自动完成：

```
New*(cfg)
    │
    ├── cfg.validate()
    │     │
    │     ├── cfg.setDefaults()  // 自动填充默认值
    │     └── 检查必填字段
    │
    └── 创建连接器实例
```

业务侧无需手动调用 SetDefaults/Validate，简化使用。

---

## 5. 健康检查设计：主动探测与状态缓存

### 5.1 双接口设计

健康检查分为两个接口：

```go
// HealthCheck：主动探测（有 I/O 开销）
func (c *redisConnector) HealthCheck(ctx context.Context) error {
    if c.client == nil {
        return xerrors.Wrapf(ErrClientNil, "redis connector[%s]", c.cfg.Name)
    }
    if err := c.client.Ping(ctx).Err(); err != nil {
        c.healthy.Store(false)
        return xerrors.Wrapf(ErrHealthCheck, "redis connector[%s]: %v", c.cfg.Name, err)
    }
    c.healthy.Store(true)
    return nil
}

// IsHealthy：状态查询（无 I/O）
func (c *redisConnector) IsHealthy() bool {
    return c.healthy.Load()
}
```

### 5.2 使用场景

| 场景              | 使用方法      | 原因                          |
|:------------------|:-------------|:------------------------------|
| 定时健康检查       | HealthCheck  | 定期更新状态，如 30 秒一次      |
| 请求前快速判断     | IsHealthy    | 避免每次请求都 Ping            |
| K8s 存活探针       | HealthCheck  | 需要实际探测                   |
| 业务逻辑降级       | IsHealthy    | 快速决策，不增加延迟           |

### 5.3 定时健康检查模式

```go
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        if err := conn.HealthCheck(ctx); err != nil {
            logger.Warn("connector health check failed", clog.Error(err))
        }
    }
}()
```

---

## 6. 可观测性集成：日志与错误处理

### 6.1 日志集成

通过 `WithLogger` 选项注入 `clog.Logger`，自动添加连接器命名空间：

```go
conn, err := connector.NewRedis(&cfg.Redis,
    connector.WithLogger(logger))
```

日志输出示例：

```json
{
    "namespace": "connector",
    "connector": "redis",
    "name": "primary",
    "msg": "attempting to connect to redis",
    "addr": "127.0.0.1:6379"
}
```

日志字段自动包含：
- `namespace`: "connector"（固定值）
- `connector`: 连接器类型（redis/mysql/etcd 等）
- `name`: 连接器名称（来自配置）

### 6.2 错误处理

使用 Genesis `xerrors` 组件提供一致的错误类型：

```go
var (
    ErrNotConnected  = xerrors.New("connector: not connected")
    ErrAlreadyClosed = xerrors.New("connector: already closed")
    ErrConnection    = xerrors.New("connector: connection failed")
    ErrTimeout       = xerrors.New("connector: timeout")
    ErrConfig        = xerrors.New("connector: invalid config")
    ErrClientNil     = xerrors.New("connector: client is nil")
    ErrHealthCheck   = xerrors.New("connector: health check failed")
)
```

错误处理模式：

```go
// 检查特定错误类型
if xerrors.Is(err, connector.ErrConnection) {
    // 处理连接失败
}

// 错误包装
return xerrors.Wrapf(err, "redis connector[%s]: connection failed", c.cfg.Name)
```

---

## 7. 实现细节：Redis 连接器示例

以下以 Redis 连接器为例说明完整实现。

### 7.1 结构定义

```go
type redisConnector struct {
    cfg     *RedisConfig
    client  *redis.Client
    logger  clog.Logger
    healthy atomic.Bool
    mu      sync.RWMutex
}
```

### 7.2 New 实现

```go
func NewRedis(cfg *RedisConfig, opts ...Option) (RedisConnector, error) {
    if err := cfg.validate(); err != nil {
        return nil, xerrors.Wrapf(err, "invalid redis config")
    }

    opt := &options{}
    for _, o := range opts {
        o(opt)
    }
    opt.applyDefaults()

    c := &redisConnector{
        cfg:    cfg,
        logger: opt.logger.With(
            clog.String("connector", "redis"),
            clog.String("name", cfg.Name),
        ),
    }

    return c, nil
}
```

### 7.3 Connect 实现

```go
func (c *redisConnector) Connect(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    // 幂等：已连接则直接返回
    if c.client != nil {
        return nil
    }

    c.logger.Info("attempting to connect to redis",
        clog.String("addr", c.cfg.Addr))

    // 创建客户端
    c.client = redis.NewClient(&redis.Options{
        Addr:         c.cfg.Addr,
        Password:     c.cfg.Password,
        DB:           c.cfg.DB,
        PoolSize:     c.cfg.PoolSize,
        MinIdleConns: c.cfg.MinIdleConns,
        DialTimeout:  c.cfg.DialTimeout,
        ReadTimeout:  c.cfg.ReadTimeout,
        WriteTimeout: c.cfg.WriteTimeout,
    })

    // 测试连接
    if err := c.client.Ping(ctx).Err(); err != nil {
        c.logger.Error("failed to connect to redis", clog.Error(err))
        c.client.Close()
        c.client = nil
        return xerrors.Wrapf(ErrConnection, "redis connector[%s]: %v", c.cfg.Name, err)
    }

    c.healthy.Store(true)
    c.logger.Info("successfully connected to redis",
        clog.String("addr", c.cfg.Addr))

    return nil
}
```

### 7.4 Close 实现

```go
func (c *redisConnector) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.logger.Info("closing redis connection")
    c.healthy.Store(false)

    if c.client == nil {
        return nil
    }

    if err := c.client.Close(); err != nil {
        c.logger.Error("failed to close redis connection", clog.Error(err))
        return err
    }

    c.client = nil
    c.logger.Info("redis connection closed successfully")
    return nil
}
```

### 7.5 GetClient 实现

```go
func (c *redisConnector) GetClient() *redis.Client {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.client
}
```

注意：返回的是原始客户端指针的副本，调用方可以直接使用，但不应 Close 它。

---

## 8. 与上层组件的协作

连接器为上层组件提供底层连接支持：

```go
func main() {
    ctx := context.Background()

    // 1. 创建连接器（应用层拥有）
    redisConn, _ := connector.NewRedis(&cfg.Redis,
        connector.WithLogger(logger))
    defer redisConn.Close()
    redisConn.Connect(ctx)

    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL,
        connector.WithLogger(logger))
    defer mysqlConn.Close()
    mysqlConn.Connect(ctx)

    // 2. 创建业务组件（注入连接器，借用客户端）
    cache, _ := cache.New(redisConn, &cfg.Cache)
    db, _ := db.New(mysqlConn, &cfg.DB)
    locker, _ := dlock.New(&cfg.DLock, dlock.WithRedisConnector(redisConn))

    // 3. 使用组件
    userSvc := service.NewUserService(db, cache, locker)
}
```

上层组件的 `Close()` 通常是 no-op，因为它们不拥有连接：

```go
// cache/cache.go
func (c *Cache) Close() error {
    // no-op: 不负责关闭 redisConn
    return nil
}
```

---

## 9. 测试策略：testcontainers 集成测试

`connector` 使用 [testcontainers](https://golang.testcontainers.org/) 进行集成测试，确保与真实服务的兼容性。

### 9.1 测试组织

```
connector/
├── connector_test.go      // 单元测试（配置验证等）
└── integration_test.go    // 集成测试（使用 testcontainers）
```

### 9.2 测试模式

每个连接器的集成测试覆盖：

1. **完整生命周期**：New → Connect → Use → HealthCheck → Close
2. **幂等性**：多次 Connect/Close 是安全的
3. **基本操作**：使用真实客户端执行操作
4. **健康检查**：成功与失败场景

示例（Redis）：

```go
func TestRedisConnectorIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test in short mode")
    }

    t.Run("完整生命周期", func(t *testing.T) {
        // 启动容器
        container, cfg := setupRedisContainer(t)
        defer container.Terminate(context.Background())

        // 创建连接器
        conn, err := NewRedis(cfg, WithLogger(getTestLogger()))
        require.NoError(t, err)

        // 连接
        ctx := context.Background()
        err = conn.Connect(ctx)
        require.NoError(t, err)
        assert.True(t, conn.IsHealthy())

        // 使用
        client := conn.GetClient()
        err = client.Set(ctx, "test:key", "value", 0).Err()
        require.NoError(t, err)

        // 健康检查
        err = conn.HealthCheck(ctx)
        require.NoError(t, err)

        // 关闭
        err = conn.Close()
        require.NoError(t, err)
        assert.False(t, conn.IsHealthy())
    })
}
```

### 9.3 运行测试

```bash
# 运行所有测试
go test ./connector/... -v

# 跳过集成测试（快速模式）
go test ./connector/... -short -v

# 只运行特定连接器
go test ./connector/... -v -run TestPostgreSQL
```

---

## 10. 支持的连接器类型

| 类型          | 接口                    | 底层客户端            | 工厂函数         |
|:--------------|:------------------------|:--------------------|:----------------|
| **Redis**     | `RedisConnector`       | `*redis.Client`      | `NewRedis`      |
| **MySQL**     | `MySQLConnector`       | `*gorm.DB`           | `NewMySQL`      |
| **PostgreSQL**| `PostgreSQLConnector`  | `*gorm.DB`           | `NewPostgreSQL` |
| **SQLite**    | `SQLiteConnector`      | `*gorm.DB`           | `NewSQLite`     |
| **Etcd**      | `EtcdConnector`        | `*clientv3.Client`   | `NewEtcd`       |
| **NATS**      | `NATSConnector`        | `*nats.Conn`         | `NewNATS`       |
| **Kafka**     | `KafkaConnector`       | `*kgo.Client`        | `NewKafka`      |

---

## 11. 最佳实践

### 11.1 分离创建与连接

在应用启动阶段先调用 `New*` 验证配置（Fail-fast），然后在系统就绪后再调用 `Connect`：

```go
func main() {
    // 启动阶段：验证配置
    redisConn, err := connector.NewRedis(&cfg.Redis)
    if err != nil {
        log.Fatalf("invalid redis config: %v", err)
    }

    // 等待依赖服务...
    time.Sleep(5 * time.Second)

    // 系统就绪：建立连接
    if err := redisConn.Connect(ctx); err != nil {
        log.Fatalf("failed to connect: %v", err)
    }
}
```

### 11.2 必须使用 defer Close

始终使用 `defer` 确保资源释放，即使在 panic 场景下也能正确关闭：

```go
conn, err := connector.NewRedis(&cfg.Redis)
if err != nil {
    return err
}
defer conn.Close() // 必须释放
```

### 11.3 单例使用

在微服务中，每个数据源应只创建一个 Connector 实例并在组件间共享：

```go
// ✅ 正确：单例共享
redisConn := connector.NewRedis(&cfg.Redis)
cache1 := cache.New(redisConn, &cfg.Cache1)
cache2 := cache.New(redisConn, &cfg.Cache2)

// ❌ 错误：重复创建
cache1 := cache.New(connector.NewRedis(&cfg.Redis1), &cfg.Cache1)
cache2 := cache.New(connector.NewRedis(&cfg.Redis2), &cfg.Cache2)
```

### 11.4 注入依赖

务必通过 `WithLogger` 注入日志组件，以便进行线上监控和排障：

```go
// 未注入日志：静默模式，排障困难
conn, _ := connector.NewRedis(&cfg.Redis)

// 注入日志：记录连接状态，便于排障
conn, _ := connector.NewRedis(&cfg.Redis,
    connector.WithLogger(logger))
```

### 11.5 错误处理

使用 `xerrors.Is()` 检查特定错误类型，进行精确的错误处理：

```go
if err := conn.Connect(ctx); err != nil {
    if xerrors.Is(err, connector.ErrConnection) {
        // 连接失败：可能是网络问题或服务不可用
    } else if xerrors.Is(err, connector.ErrConfig) {
        // 配置错误：程序bug，需修复配置
    } else {
        // 其他错误
    }
}
```

---

## 12. 设计权衡与未来方向

### 12.1 当前设计的权衡

| 决策 | 权衡 |
|:-----|:-----|
| 两阶段初始化 | 增加一行代码，但实现 Fail-fast 和灵活连接时机 |
| 借用模型 | 上层组件 Close 是 no-op，但避免了资源所有权混乱 |
| 健康状态缓存 | 减少下游压力，但状态可能有短暂延迟 |
| testcontainers | 测试更真实，但需要 Docker 环境 |

### 12.2 可能的扩展方向

- **MongoDB Connector**：文档数据库支持。
- **Elasticsearch Connector**：搜索引擎支持。
- **RabbitMQ Connector**：AMQP 协议消息队列支持。
- **连接池指标**：导出连接池使用率、等待队列长度等指标。
- **连接预热**：启动时主动建立 N 个连接，避免首请求延迟。

---

## 13. 总结

`connector` 组件的核心价值在于：

1. **统一抽象**：隐藏不同客户端库的差异，提供一致的初始化、连接、健康检查语义。
2. **资源所有权明确**：谁创建谁释放，借用模型避免资源泄露和双重关闭。
3. **Fail-fast**：配置验证在启动阶段完成，连接失败快速暴露。
4. **可观测性集成**：与 `clog`/`xerrors` 深度集成，统一日志命名空间和错误类型。
5. **类型安全**：泛型接口提供编译时类型检查，避免运行时类型断言。

在微服务架构中，外部连接管理是"基础设施"级别的能力。`connector` 组件的设计目标是让业务开发者"不需要关心连接细节"，只需要"创建、连接、使用、关闭"即可。
