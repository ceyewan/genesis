# Genesis connector：微服务外部连接管理的设计与实现

Genesis `connector` 是基础设施层（L1）的核心组件，负责管理与外部服务的原始连接，包括 MySQL、PostgreSQL、SQLite、Redis、Etcd、NATS、Kafka 七种后端。它的目标不是再包一层客户端 API，而是在 Genesis 组件体系里统一连接的生命周期语义：怎么初始化、什么时候连接、如何探活、谁来关闭。这篇文章重点讲 `connector` 为什么这样设计、做了哪些取舍，而不只是用法介绍。

## 0 摘要

- `connector` 不是客户端封装，而是对连接生命周期语义的统一：New 验证配置，Connect 建立 I/O，Close 释放资源
- 两阶段初始化让配置错误在启动阶段暴露，I/O 错误在连接阶段暴露，而不是藏到第一次请求时才炸
- 借用模型明确所有权：连接器自己关闭底层连接，上层组件（cache、dlock、mq）只借用客户端，不应关闭连接器
- 健康检查的 HealthCheck 和 IsHealthy 分开，一个有 I/O、一个读缓存，避免正常请求承担探活开销
- 泛型接口 `TypedConnector[T]` 让 `GetClient()` 在编译期就确定类型，不需要运行时断言
- 七种连接器直接暴露各自的底层客户端类型，这是一个刻意的取舍：连接器层管生命周期，不管 API 包装

---

## 1 背景与问题

微服务通常需要同时管理多个外部依赖，常见的组合包括：Redis 做缓存和分布式锁、MySQL 或 PostgreSQL 做主存储、Etcd 做配置中心或服务发现、NATS 或 Kafka 做消息队列。每种外部系统背后都有一个专属的客户端库，而这些客户端库在连接建立方式上差异很大。Redis 客户端在 `NewClient` 时不立即连接，而是在第一次请求时才真正触发；GORM 在 `Open` 时就建立连接；etcd 的 `clientv3.New` 会阻塞直到连接超时；franz-go 创建客户端时也不立即连接，`Ping` 才是真正的握手。

直接在业务代码里管理这些客户端，问题很快就出现了。首先是初始化模式不统一，有的要显式 Connect，有的在构造函数里隐式连接，调用方需要记住每个库的行为差异。其次是资源管理散乱，连接池配置、优雅关闭、重用逻辑分散在各个业务组件里，如果多个组件各自持有同一个数据库的客户端实例，连接数很容易翻倍。再就是健康检查方式各异，有的提供 Ping，有的需要发一个测试请求，有的没有任何探活机制。还有可观测性缺失的问题，缺少统一的日志命名空间，日志里很难看出这条日志来自哪个连接器实例。

Genesis 需要一个统一的连接管理层，不是为了再包一层 API，而是为了让连接这件事在整个组件体系里有一致的初始化、探活和关闭语义。

---

## 2 设计目标

`connector` 的设计目标可以归纳为五条，后文所有接口取舍和实现决策都能在这里找到依据：

- **显式优于隐式**：New 负责验证，Connect 负责 I/O，没有隐式的后台连接或惰性初始化。调用方清楚地知道连接什么时候建立。
- **所有权清晰**：谁创建连接器，谁负责 Close。上层组件只是借用者，不拥有连接的生命周期，也不应该调用 Close。
- **Fail-fast**：配置错误在 New 时就暴露，I/O 错误在 Connect 时就暴露。不把问题推迟到第一次业务请求。
- **低可观测性负担**：日志命名空间、连接器名称、错误包装由 connector 统一托管，业务层无需重复埋点。
- **接口克制**：Connector 接口只定义连接管理相关的方法，不扩展业务能力。Hash、ZSet、消息路由等高级 API 属于上层组件的职责。

这五条目标决定了 `connector` 不会是一个功能繁多的"超级客户端"。它刻意保持小接口，优先保证契约清晰、行为可预测。

---

## 3 核心接口与配置

### 3.1 Connector 基础接口

所有连接器都实现一个共同的基础接口：

```go
type Connector interface {
    Connect(ctx context.Context) error
    Close() error
    HealthCheck(ctx context.Context) error
    IsHealthy() bool
    Name() string
}
```

这五个方法的边界划分很清楚。`Connect` 和 `Close` 管生命周期，都是幂等的，可以安全地重复调用。`HealthCheck` 是主动探测，每次调用都会向远端发送请求并更新内部的健康状态缓存；`IsHealthy` 则直接返回缓存的结果，无任何 I/O 开销。`Name` 返回连接器名称，主要用于日志标识。

这里有一个需要特别说明的设计决定：`Connect` 是幂等的，多次调用不会报错。这样做的原因是方便调用方实现重试逻辑，而不需要先判断"当前是否已连接"再决定是否调用。

### 3.2 TypedConnector[T] 泛型接口

```go
type TypedConnector[T any] interface {
    Connector
    GetClient() T
}
```

`TypedConnector[T]` 在 `Connector` 基础上增加了 `GetClient()` 方法，返回类型由类型参数 T 确定。各种具体的连接器接口都是它的实例化：

| 接口 | 类型参数 T | 工厂函数 |
|------|------------|----------|
| `RedisConnector` | `*redis.Client` | `NewRedis` |
| `MySQLConnector` | `*gorm.DB` | `NewMySQL` |
| `PostgreSQLConnector` | `*gorm.DB` | `NewPostgreSQL` |
| `SQLiteConnector` | `*gorm.DB` | `NewSQLite` |
| `EtcdConnector` | `*clientv3.Client` | `NewEtcd` |
| `NATSConnector` | `*nats.Conn` | `NewNATS` |
| `KafkaConnector` | `*kgo.Client` | `NewKafka` |

这样设计的收益是编译期类型确定。调用 `redisConn.GetClient()` 直接得到 `*redis.Client`，不需要类型断言，编译器会在接口实现时检查类型是否匹配。

### 3.3 配置设计

所有配置结构都采用扁平化原则，不使用嵌套子结构，所有字段平铺在顶层。必填字段（如 Redis 的 `Addr`、MySQL 的 `Host`/`Username`/`Database`）在 `validate` 里检查；可选字段通过 `setDefaults` 自动填充合理默认值。这个处理流程在 `New` 函数内部自动完成，调用方不需要手动调用 `SetDefaults` 或 `Validate`。

对于 MySQL 和 PostgreSQL，可以直接传入完整 DSN，优先级高于独立字段。这是为了支持需要完全控制连接字符串的场景，例如配置了 TLS 证书、连接选项较多，或者密码包含特殊字符时直接传入已转义好的 DSN。

---

## 4 核心概念与数据模型

### 两阶段初始化

Genesis connector 的两阶段初始化是整个设计的基石。`New` 阶段只做一件事：验证配置并创建连接器实例，不发起任何网络请求；`Connect` 阶段才真正建立底层连接、发送 Ping 或 Auth 验证连通性。

这种分离带来的直接好处是 Fail-fast。配置错误在应用启动阶段就能被发现，不必等到第一次业务请求。而且 `Connect` 的调用时机完全由应用层控制，可以在健康检查通过后才连接，也可以在等待依赖服务启动后再连接，而不是在 `New` 里无条件阻塞。

### 借用模型

`connector` 采用明确的借用模型来管理资源所有权。连接器本身是 Owner，它拥有底层连接的生命周期，在应用退出时调用 `Close()` 释放。上层组件，例如 `cache`、`dlock`、`mq` 等，是 Borrower，它们通过 `GetClient()` 拿到客户端引用，使用完成后不调用任何关闭方法，因为它们不拥有这个资源。

这个规则解决了一个实际问题：如果多个组件都可以关闭同一个连接，就会发生双重关闭；如果没有人管关闭，就会发生资源泄露。借用模型通过明确所有权消除了两种问题。实践上，应用层用 `defer` 保证按 LIFO 顺序关闭，先关闭借用组件（如果它们有 Close），再关闭连接器。

### 健康检查的双接口设计

`HealthCheck` 和 `IsHealthy` 分开是一个刻意的接口设计。每次调用 `HealthCheck` 都会向远端发送测试请求，有真实的 I/O 开销，适合放在定时 goroutine 里定期执行，或者作为 K8s 的 liveness probe 调用。`IsHealthy` 则读取上一次 `HealthCheck` 留下的缓存状态（一个 `atomic.Bool`），没有任何 I/O，适合在业务请求路径上快速判断服务是否可用。

如果把这两个场景合并成一个接口，必然会做出错误的妥协：要么业务路径每次都做 I/O 探活，要么定时健康检查也只读缓存、无法发现真实故障。分开才能让两个使用场景各取所需。

---

## 5 关键实现思路

### 5.1 并发安全与状态管理

所有连接器都用 `sync.RWMutex` 保护内部的连接状态（`db`、`client`、`conn` 等字段）。写路径（`Connect`、`Close`）持有写锁，读路径（`GetClient`、`HealthCheck` 内部的客户端读取）持有读锁，允许多个 goroutine 并发调用 `GetClient`。

健康状态用 `atomic.Bool` 单独管理，`IsHealthy` 直接原子读，不经过互斥锁，确保状态查询不会被持有写锁的 Connect/Close 操作阻塞。

`Connect` 和 `Close` 的幂等性靠简单的 nil 检查实现：`Connect` 发现 `client != nil` 就直接返回，`Close` 发现 `client == nil` 也直接返回。这比维护一个显式的状态机（DISCONNECTED / CONNECTING / CONNECTED / CLOSED）简单得多，也足够满足实际需求。

### 5.2 DSN 安全构造

MySQL 和 PostgreSQL 都支持从字段自动拼 DSN。早期版本直接用 `fmt.Sprintf` 组装，但这在密码包含 `@`、空格、`/` 等特殊字符时会导致 DSN 解析错误。

MySQL 现在改为使用 `github.com/go-sql-driver/mysql` 提供的 `Config.FormatDSN()`，它负责正确处理密码中的特殊字符；PostgreSQL 改为用标准库 `net/url.URL` 结合 `url.UserPassword()` 构造 URL 格式的连接串，同样确保用户名和密码得到正确的 URL 编码。两种方式都让连接字符串的构造从"手工拼接字符串"变成了"结构化构造"，规避了一类容易遗漏的边界问题。

### 5.3 Close 的最佳努力语义

关闭操作采用最佳努力（best-effort）语义：无论底层 Close 是否出错，连接器的 `client` 字段都会立即置为 `nil`。这样做的原因是，如果 Close 失败但 client 没有置 nil，下次调用 Close 会再次尝试关闭一个可能已处于损坏状态的连接；而置 nil 之后，重复 Close 直接返回 nil，语义清晰。代码结构上是先 `db := c.db; c.db = nil`，再对 `db` 执行 Close，而不是 Close 成功后才 nil。

---

## 6 工程取舍与设计权衡

### 6.1 为什么不在 New 里直接建立连接

最直观的 API 是：创建实例的同时就连接好，直接可用。很多库确实这样做，例如 GORM 的 `Open`。但 Genesis connector 选择分开，原因有两个。

第一个是 Fail-fast 的精确性。如果 New 就连接，配置错误和网络问题会混在同一个错误里，调用方没法判断是"我写错了配置"还是"服务现在不可用"。分开之后，New 报错意味着配置 bug，Connect 报错意味着 I/O 问题，错误语义更清晰。

第二个是灵活的连接时机。在实际项目里，经常需要先初始化所有连接器实例（验证配置），等服务发现就绪、或者依赖服务就绪之后才真正连接。如果 New 就连接，这个时序控制就没法做了。

### 6.2 为什么借用者不关闭连接器

`cache`、`dlock` 等组件的 `Close()` 方法通常只处理它们自己拥有的资源（如 otter 缓存的后台 goroutine），而不关闭注入进来的连接器。

这是借用模型的直接结果。如果 `cache.Close()` 也关闭了 Redis 连接器，那么应用里其他共享这个连接器的组件（例如 `dlock`）就会因为底层连接被意外关闭而出错。更重要的是，连接器是应用层创建的，关闭权也应该留给应用层，而不是让借用者决定谁的生命周期何时结束。

这个设计让"谁创建、谁释放"的原则在整个组件体系里保持一致，而不是只靠文档约定。

### 6.3 为什么暴露第三方类型而不是通用抽象

`GetClient()` 返回的是 `*redis.Client`、`*gorm.DB`、`*kgo.Client` 这些第三方库的原始类型，而不是 Genesis 自己定义的通用抽象。这看起来像是把第三方依赖泄漏到了接口层，其实是一个刻意的取舍。

如果 connector 层对 `*gorm.DB` 做一层封装，就必须决定封装哪些方法、不封装哪些方法。GORM 的 API 足够复杂，任何封装层都会面临"要么暴露太多"或"要么功能不够用"的两难。最终的结果往往是一个表面上解耦、实际上只会增加维护负担的 thin wrapper。

connector 层的职责是连接生命周期，不是 API 包装。上层组件需要高级 API 时，直接通过 `GetClient()` 拿到原始客户端就好。这让 connector 层保持克制，不承担它不该承担的职责。

### 6.4 为什么没有自动重连

部分连接器（NATS）有内置的自动重连机制，但 connector 层本身不提供统一的重连框架。这个决定背后有一个判断：对于数据库（MySQL、PostgreSQL），静默重连可能掩盖连接中断期间丢失的事务或数据不一致；对于 Etcd，重连期间的 watch 状态需要应用层自己处理；对于 Redis，go-redis 本身在连接池层已经处理了大部分短暂断线的情况。

统一的重连框架听起来很美，但实际上每种后端的重连语义都不同，一个统一框架要么太简陋（只是 sleep 后重试 Connect），要么为了覆盖所有场景变得很复杂。当前的选择是：让使用者在应用层显式控制重连时机，connector 只保证 Connect 是幂等的、可以安全重复调用。

---

## 7 适用场景与实践建议

`connector` 适合以下场景：你在构建微服务，需要统一管理多个外部依赖的连接生命周期；你希望全项目共享一套连接管理约定，包括日志命名空间和错误类型；你需要清晰的资源所有权，避免多组件共用连接时出现双重关闭或资源泄露。

它不适合以下场景：你只需要一个简单的脚本访问 Redis，直接用 go-redis 会更轻量；你需要在同一个连接器上动态切换后端，connector 的接口是面向单一后端设计的；你对 GORM 或 franz-go 有深度定制需求，这时候直接管理底层客户端比通过 connector 更灵活。

实践上有四条推荐。

第一，Always 用 `defer` 确保 Close，即使在 panic 场景下也能正确关闭。连接器由应用层的 main 或服务初始化代码创建，关闭责任也留在这里，不要下放给业务组件：

```go
redisConn, err := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
if err != nil {
    return err
}
defer redisConn.Close()

if err := redisConn.Connect(ctx); err != nil {
    return err
}
```

第二，每种数据源在整个服务里只创建一个连接器实例，通过依赖注入共享给多个组件。为每个组件各自创建独立的连接器是反模式，会导致连接数爆炸。

第三，务必通过 `WithLogger` 注入日志，否则连接建立、关闭、健康检查的所有事件都是静默的，线上排障时会非常困难。不注入也不会 panic，logger 会默认降级为 Discard。

第四，定时 HealthCheck 加 IsHealthy 快速判断是推荐的探活组合，而不是在业务请求路径上每次都调用 HealthCheck：

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

常见误区集中在几个地方：在上层组件的 Close 里调用连接器的 Close（会导致其他共享者无法使用）；在 init 函数里直接 Connect（启动失败时无法优雅退出）；为每个业务方法各自创建临时连接器（极度浪费连接资源）；忘记 WithLogger 导致线上问题排查困难。

---

## 8 总结

`connector` 的价值不在于"封装了哪些客户端库"，而在于它把 Genesis 对连接生命周期的工程共识固化成了一套统一的接口和行为。两阶段初始化让 Fail-fast 成为默认行为，借用模型让资源所有权在组件体系里保持清晰，双接口健康检查让探活开销不影响正常请求路径。

如果要用一句话概括 `connector` 的设计原则：**管好连接的生命周期，其他的都不是 connector 该操心的事。**
