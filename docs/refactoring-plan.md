# Genesis 重构与审计计划 (Genesis Refactoring & Audit Plan)

> NOTE: 本文档为当前重构的执行计划（source-of-truth）。在进行代码重构或调整架构时，应以本文件的约定为优先；其他设计文档（如 `genesis-design.md`、`component-spec.md`）为参考。任何偏离本计划的重要决策，应记录在 `docs/reviews/architecture-decisions.md`。

**目标**：将 Genesis 从原型集合转变为生产级、符合 Go 习惯的微服务基座库。
**核心原则**：**显式优于隐式**（Explicit over Implicit）、**简单优于聪明**（Simple over Clever）、**组合优于继承**（Composition over Inheritance）。

---

## 1. 核心架构决策：移除 DI 容器

### 1.1. 决策背景

原设计采用了 `pkg/container` 作为 DI 容器，存在以下问题：

| 问题 | 描述 | Go 哲学冲突 |
|------|------|-------------|
| **服务定位器反模式** | 业务代码从 Container "拉取" 依赖 (`app.DB`, `app.DLock`) | 依赖应该"推入"，而非"拉取" |
| **隐藏依赖关系** | 函数签名看不出真实依赖 | Go 强调显式声明 |
| **运行时魔法** | Phase 排序、动态组件创建 | Go 偏好编译时检查 |
| **测试困难** | 需要 Mock 整个 Container | 应只 Mock 实际依赖的接口 |

### 1.2. 决策结论

**删除 `pkg/container`，采用 Go Native 的显式依赖注入。**

Genesis 的定位是**组件库**，而非**框架**。我们提供积木，用户自己搭建。

---

## 2. 总体架构：三层模型 (Three-Layer Architecture)

移除 Glue Layer 后，Genesis 简化为三层：

| 层次 | 核心组件 | 职责 | 目录结构 |
|:-----|:---------|:-----|:---------|
| **Level 3: Governance** | `ratelimit`, `breaker`, `registry`, `telemetry` | 流量治理，切面能力 | `pkg/{comp}` |
| **Level 2: Business** | `cache`, `idgen`, `dlock`, `idempotency`, `mq` | 业务能力封装 | `pkg/{comp}` |
| **Level 1: Infrastructure** | `connector`, `db` | 连接管理，底层 I/O | `pkg/{comp}` + `internal/{comp}` |
| **Level 0: Base** | `clog`, `config`, `xerrors` | 框架基石 | `pkg/{comp}` |

---

## 3. Go Native 依赖注入设计

### 3.1. 核心原则

```text
┌─────────────────────────────────────────────────────────────────┐
│                    Go Native 依赖注入                            │
├─────────────────────────────────────────────────────────────────┤
│  1. 构造函数注入：依赖通过 New() 参数传入                         │
│  2. 显式调用：main.go 中手写初始化代码，依赖关系一目了然           │
│  3. defer 释放：利用 Go 的 defer 机制，自然实现 LIFO 关闭顺序     │
│  4. 接口隔离：业务层只依赖需要的接口，而非具体实现                 │
└─────────────────────────────────────────────────────────────────┘
```

### 3.2. 标准初始化模式

所有使用 Genesis 的应用应遵循以下模式：

```go
// main.go - 显式、清晰、可读
package main

import (
    "context"
    "log"
    "os/signal"
    "syscall"
    
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/config"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/db"
    "github.com/ceyewan/genesis/pkg/dlock"
    "github.com/ceyewan/genesis/pkg/cache"
    "github.com/ceyewan/genesis/pkg/telemetry"
)

func main() {
    // 0. 信号处理
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // 1. 加载配置（最先，其他组件依赖它）
    cfg, err := config.Load("config.yaml")
    if err != nil {
        log.Fatalf("load config: %v", err)
    }

    // 2. 初始化 Logger（基础设施，几乎所有组件都需要）
    logger, err := clog.New(&cfg.Log)
    if err != nil {
        log.Fatalf("create logger: %v", err)
    }

    // 3. 初始化 Telemetry（可选，用于 Metrics/Tracing）
    tel, err := telemetry.New(&cfg.Telemetry)
    if err != nil {
        log.Fatalf("create telemetry: %v", err)
    }
    defer tel.Shutdown(ctx)

    // 4. 创建 Connectors（按依赖顺序，defer 自动逆序关闭）
    redisConn, err := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    if err != nil {
        log.Fatalf("create redis connector: %v", err)
    }
    defer redisConn.Close()

    mysqlConn, err := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
    if err != nil {
        log.Fatalf("create mysql connector: %v", err)
    }
    defer mysqlConn.Close()

    // 5. 创建组件（注入 Connector + Logger + Telemetry）
    database, err := db.New(mysqlConn, &cfg.DB,
        db.WithLogger(logger),
        db.WithMeter(tel.Meter()),
        db.WithTracer(tel.Tracer()),
    )
    if err != nil {
        log.Fatalf("create db: %v", err)
    }

    locker, err := dlock.NewRedis(redisConn, &cfg.DLock,
        dlock.WithLogger(logger),
        dlock.WithMeter(tel.Meter()),
    )
    if err != nil {
        log.Fatalf("create dlock: %v", err)
    }

    cacheClient, err := cache.New(redisConn, &cfg.Cache,
        cache.WithLogger(logger),
    )
    if err != nil {
        log.Fatalf("create cache: %v", err)
    }

    // 6. 创建业务服务（注入组件接口）
    userSvc := service.NewUserService(database, locker)
    orderSvc := service.NewOrderService(database, cacheClient)

    // 7. 启动服务器
    server := api.NewServer(userSvc, orderSvc)
    if err := server.Run(ctx); err != nil {
        logger.Error("server error", "err", err)
    }
}
```

### 3.3. 为什么这样更好？

| 特性 | Container 模式 | Go Native 模式 |
|------|----------------|----------------|
| **依赖可见性** | 隐藏在 Container 内部 | main.go 中一目了然 |
| **编译时检查** | 运行时才发现缺少依赖 | 编译时检查完整性 |
| **测试友好** | 需要 Mock Container | 只 Mock 需要的接口 |
| **学习成本** | 需要理解 Container 机制 | 标准 Go 代码，无需学习 |
| **调试体验** | 堆栈包含 Container 内部 | 堆栈清晰直接 |
| **IDE 支持** | 难以跳转到实际实现 | 完美支持 Go to Definition |

### 3.4. 生命周期管理

不再需要 `Lifecycle` 接口和 `Phase` 机制。利用 Go 的 `defer` 自然实现：

```go
// 创建顺序：Config -> Logger -> Connector -> Component -> Service
// 关闭顺序：Service -> Component -> Connector -> Logger (defer 自动逆序)

func main() {
    cfg := config.MustLoad("config.yaml")
    
    logger := clog.Must(clog.New(&cfg.Log))
    // defer logger.Sync() // 如果需要
    
    redisConn := connector.MustNewRedis(&cfg.Redis)
    defer redisConn.Close() // 第 3 个关闭
    
    mysqlConn := connector.MustNewMySQL(&cfg.MySQL)
    defer mysqlConn.Close() // 第 2 个关闭
    
    dlock := dlock.MustNewRedis(redisConn, &cfg.DLock)
    // dlock 无状态，无需 Close
    
    server := api.NewServer(dlock)
    defer server.Shutdown(ctx) // 第 1 个关闭
    
    server.Run(ctx)
}
```

### 3.5. 大型项目：可选使用 Wire

对于 30+ 个服务的大型项目，可选择使用 [Google Wire](https://github.com/google/wire) 自动生成初始化代码。

**Wire 的本质**：它只是一个代码生成器，生成的代码和手写的一模一样。

```go
// wire.go (开发者编写)
//go:build wireinject

package main

import "github.com/google/wire"

func InitializeApp(cfg *Config) (*App, func(), error) {
    wire.Build(
        connector.NewRedis,
        connector.NewMySQL,
        db.New,
        dlock.NewRedis,
        cache.New,
        service.NewUserService,
        service.NewOrderService,
        api.NewServer,
        wire.Struct(new(App), "*"),
    )
    return nil, nil, nil
}
```

运行 `wire` 后生成 `wire_gen.go`，内容就是标准的手写初始化代码。

**建议**：先手写，等觉得繁琐了再引入 Wire。

---

## 4. Connector 简化与资源所有权

### 4.1. 移除 Lifecycle 接口

原设计中 Connector 继承了 `Lifecycle` 接口（`Start/Stop/Phase`），这是为 Container 服务的。现在删除 Container 后，简化为：

```go
// Before: 过度设计
type Connector interface {
    Lifecycle  // Start/Stop/Phase - 冗余！
    Connect(ctx context.Context) error
    Close() error
    HealthCheck(ctx context.Context) error
    IsHealthy() bool
    Name() string
}

// After: 最小接口
type Connector interface {
    Connect(ctx context.Context) error   // 建立连接
    Close() error                         // 关闭连接
    HealthCheck(ctx context.Context) error
    IsHealthy() bool
    Name() string
}
```

**移除的内容**：
- `Start()` → 与 `Connect()` 重复
- `Stop()` → 与 `Close()` 重复
- `Phase()` → 只为 Container 服务，不再需要
- `Lifecycle` 接口定义

### 4.2. 资源所有权原则

**核心原则：谁创建，谁负责关闭。**

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

### 4.3. 组件 Close() 规范

所有**借用** Connector 的组件，其 `Close()` 方法应为 **no-op**：

```go
// pkg/cache/redis.go
func (c *redisCache) Close() error {
    // No-op: Cache 不拥有 Redis 连接，由 Connector 管理
    // 调用方应关闭 Connector 而非 Cache
    return nil
}

// pkg/dlock/redis.go
func (l *redisLocker) Close() error {
    // No-op: DLock 不拥有 Redis 连接
    return nil
}

// pkg/db/db.go
func (d *database) Close() error {
    // No-op: DB 不拥有 MySQL 连接
    return nil
}
```

**为什么保留 Close() 方法？**

1. **接口一致性**：统一的资源清理接口
2. **未来扩展**：组件可能需要清理内部资源（如本地缓存、后台 goroutine）
3. **调用无害**：即使误调用也不会出错

### 4.4. 正确的使用模式

```go
func main() {
    // ✅ Connector: 必须 defer Close()
    redisConn, _ := connector.NewRedis(&cfg.Redis)
    defer redisConn.Close()  // 关闭连接池
    
    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL)
    defer mysqlConn.Close()  // 关闭连接池
    
    // ✅ Component: 无需 Close()（但调用也无害）
    cache, _ := cache.New(redisConn, &cfg.Cache)
    // defer cache.Close()  // 可选，是 no-op
    
    dlock, _ := dlock.NewRedis(redisConn, &cfg.DLock)
    // defer dlock.Close()  // 可选，是 no-op
    
    db, _ := db.New(mysqlConn, &cfg.DB)
    // defer db.Close()     // 可选，是 no-op
}
```

### 4.5. 特殊情况：组件拥有独立资源

如果组件有**自己的**后台资源（非借用），则 `Close()` 必须释放：

```go
// 例如：带本地缓存的 Cache
type localCache struct {
    redis  *redis.Client  // 借用，不关闭
    local  *bigcache.BigCache  // 自己创建，需要关闭
    ticker *time.Ticker  // 自己创建，需要停止
}

func (c *localCache) Close() error {
    // 只关闭自己拥有的资源
    c.ticker.Stop()
    return c.local.Close()
    // 不关闭 c.redis，它由 Connector 管理
}
```

### 4.6. Connector 接口最终定义

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

// 具体类型定义
type RedisConnector interface {
    TypedConnector[*redis.Client]
}

type MySQLConnector interface {
    TypedConnector[*gorm.DB]
}

type EtcdConnector interface {
    TypedConnector[*clientv3.Client]
}

type NATSConnector interface {
    TypedConnector[*nats.Conn]
}
```

---

## 5. 组件扁平化 (Flattening Strategy)

### 5.1. 适用范围

Level 2 (Business) 和 Level 3 (Governance) 组件。

### 5.2. 行动

1. **移除 `internal/{comp}`**：实现逻辑移至 `pkg/{comp}/`
2. **移除 `types` 子包**：`Config`, `Interface`, `Errors` 移至包根目录
3. **使用非导出结构体**：封装实现细节

```text
# Before
pkg/dlock/
├── dlock.go
├── options.go
└── types/
    ├── config.go
    ├── interface.go
    └── errors.go
internal/dlock/
├── redis/
└── etcd/

# After
pkg/dlock/
├── dlock.go       # 接口定义 + 工厂函数
├── config.go      # Config 结构体
├── errors.go      # Sentinel Errors
├── options.go     # Option 函数
├── redis.go       # type redisLocker struct (非导出)
└── etcd.go        # type etcdLocker struct (非导出)
```

### 5.3. 用户体验

```go
// Before: 冗长的导入路径
import (
    "github.com/ceyewan/genesis/pkg/dlock"
    "github.com/ceyewan/genesis/pkg/dlock/types"
)
locker, err := dlock.NewRedis(conn, &types.Config{...})

// After: 简洁直接
import "github.com/ceyewan/genesis/pkg/dlock"
locker, err := dlock.NewRedis(conn, &dlock.Config{...})
```

---

## 6. API 与开发规范

### 6.1. 构造函数规范

```go
// 标准签名
func New(conn Connector, cfg *Config, opts ...Option) (Interface, error)

// 规则：
// 1. 必选参数：核心依赖 (Connector) + 配置 (Config)
// 2. 可选参数：Logger/Meter/Tracer 通过 Option 注入
// 3. 禁止：New 中执行阻塞 I/O
```

### 6.2. Option 规范

```go
type options struct {
    logger clog.Logger
    meter  telemetry.Meter
    tracer telemetry.Tracer
}

type Option func(*options)

func WithLogger(l clog.Logger) Option {
    return func(o *options) {
        o.logger = l.With("component", "dlock")
    }
}

func WithMeter(m telemetry.Meter) Option {
    return func(o *options) { o.meter = m }
}

func WithTracer(t telemetry.Tracer) Option {
    return func(o *options) { o.tracer = t }
}
```

### 6.3. 接口设计规范

业务服务应依赖**最小接口**，而非具体实现：

```go
// ✅ Good: 只依赖需要的方法
type UserRepository interface {
    FindByID(ctx context.Context, id int64) (*User, error)
    Save(ctx context.Context, user *User) error
}

type UserService struct {
    repo UserRepository  // 接口，易于 Mock
}

// ❌ Bad: 依赖具体实现
type UserService struct {
    db *db.DB  // 具体类型，难以 Mock
}
```

### 6.4. 错误处理规范

```go
// pkg/dlock/errors.go
var (
    ErrLockNotHeld   = errors.New("dlock: lock not held")
    ErrLockTimeout   = errors.New("dlock: acquire timeout")
    ErrAlreadyLocked = errors.New("dlock: already locked")
)

// 使用时 Wrap 错误
return fmt.Errorf("acquire lock %s: %w", key, ErrLockTimeout)
```

---

## 7. 工程化建设

1. **Makefile**: `make test`, `make lint`, `make up`
2. **CI/CD**: `.github/workflows/ci.yml`
3. **Dev Env**: `docker-compose.dev.yml`

---

## 8. 执行路线图

| 阶段 | 任务 | 详情 |
|:-----|:-----|:-----|
| **Phase 1** | **删除 Container** | 移除 `pkg/container`，更新所有文档 |
| **Phase 2** | **简化 Connector** | 移除 `Lifecycle` 接口，简化为最小 API |
| **Phase 3** | **Pilot: Ratelimit** | 试点扁平化重构 |
| **Phase 4** | **Core L2** | 重构 `cache`, `idgen`, `dlock`, `idempotency` |
| **Phase 5** | **Infra L1** | 优化 `connector`, `db`, `mq` API |
| **Phase 6** | **Gov L3** | 重构 `registry`, `breaker`，建设 Adapter |
| **Phase 7** | **Examples** | 提供完整的 main.go 示例，展示 Go Native DI |

---

## 9. 待删除/修改内容

执行重构时，需要处理：

### 9.1. 删除

1. `pkg/container/` 目录
2. `docs/container-design.md` 文件 ✅ 已删除
3. `examples/container/` 目录
4. 所有文档中关于 "Container Mode" 的描述

### 9.2. 修改

1. **`pkg/connector/interface.go`**：移除 `Lifecycle` 接口及其继承
2. **`internal/connector/*.go`**：移除 `Start()`, `Stop()`, `Phase()` 方法
3. **所有组件的 `Close()` 方法**：确保借用 Connector 的组件实现 no-op Close

---

## 附录：Go 依赖注入最佳实践

### A.1. 为什么不用 DI 容器？

> "A little copying is better than a little dependency." — Rob Pike

Go 社区普遍认为 DI 容器是 Anti-pattern，原因：

1. **违背显式原则**：Go 代码应该一眼看出依赖关系
2. **运行时魔法**：Go 偏好编译时检查
3. **过度抽象**：解决的问题比引入的复杂度小

### A.2. 什么时候需要 Wire？

| 场景 | 建议 |
|------|------|
| < 10 个服务 | 手写 main.go |
| 10-30 个服务 | 手写或 Wire 都可以 |
| > 30 个服务 | 考虑用 Wire 减少样板代码 |

### A.3. 参考项目

- [go-kratos/kratos](https://github.com/go-kratos/kratos) - 使用 Wire
- [uber-go/fx](https://github.com/uber-go/fx) - Uber 的 DI 框架（非 Go 惯用法）
- [google/wire](https://github.com/google/wire) - 编译时 DI
