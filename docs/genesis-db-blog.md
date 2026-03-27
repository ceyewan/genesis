# Genesis db：GORM 封装组件的设计与取舍

Genesis `db` 是 L1 基础设施层组件，在 `connector` 提供的连接之上，统一封装 GORM 的初始化流程、事务管理和可观测性接入。它的目标不是重新抽象 ORM API，而是把跨项目重复出现的初始化和治理样板代码沉淀为标准组件，让业务代码继续使用原生 `*gorm.DB`，同时自动获得日志和链路追踪能力。这篇文章重点讲为什么这样设计、取舍了什么，而不只是介绍怎么用。

## 0 摘要

- `db` 对外暴露极简三方法接口：`DB(ctx)`、`Transaction(ctx, fn)`、`Close()`
- 通过 `Config.Driver` 配置驱动，支持 mysql、postgresql、sqlite，连接器由调用方显式注入
- SQL 日志自动输出到 clog，区分普通查询、慢查询（>200ms）和错误三种级别
- 注入 `WithTracer` 后，通过 otelgorm 插件为每条数据库查询自动创建 span
- `Close()` 是 no-op，资源所有权遵循借用模型，连接生命周期由 `connector` 管理
- 分表属于数据库层面的能力，推荐使用 PG / MySQL 原生分区而非应用层中间件

---

## 1 背景与问题

直接使用 GORM 的项目往往会在以下几个地方积累重复样板代码。

首先是初始化方式。不同驱动的 DSN 构造规则不同：MySQL 需要处理字符集、时区和特殊字符转义；PostgreSQL 需要正确拼接连接 URL；SQLite 需要处理内存数据库和文件路径。每个服务自行实现一遍，细节处理水平参差不齐，密码里的特殊字符常常在某个环境里悄悄注入失败。

其次是可观测性接入。SQL 日志、慢查询告警、OpenTelemetry trace 插件，几乎每个服务都要重复注册一遍，设置慢查询阈值，决定用什么字段名输出，是否接入 trace。没有统一约定时，这些配置在不同服务里高度分散且难以维护。

第三是事务管理。GORM 原生的 `Begin / Commit / Rollback` 三步式事务管理容易漏写回滚，闭包式封装虽然常见，但每个团队写出来的版本都略有不同，Context 传播方式也不一致。

`db` 组件的存在，就是把这三类问题的标准解法固化下来，让业务工程师不必每次都从头解决这些基础问题。

---

## 2 设计目标

`db` 的设计目标可以归纳为四条：

- **保留原生体验**：不引入新的查询抽象，`DB(ctx)` 直接返回 `*gorm.DB`，业务代码无需改变
- **统一初始化**：多驱动的初始化和 DSN 构造细节封装在组件内部，外部只看 Driver 字段
- **自动可观测**：SQL 日志和 trace 跟随 `WithLogger` / `WithTracer` 自动接入，零额外配置
- **清晰所有权**：连接生命周期属于 `connector`，`db` 只是借用方，边界不模糊

这四条目标决定了组件接口极简、不引入新依赖、不做过度抽象。

---

## 3 核心接口与配置

`db` 的公开接口刻意保持极简，只有三个方法：

```go
type DB interface {
    DB(ctx context.Context) *gorm.DB
    Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error
    Close() error
}
```

`DB(ctx)` 是最常用的方法，它调用 `gorm.DB.WithContext(ctx)` 把当前 Context 注入到查询链路，保证日志字段、trace span 和超时控制能在一条查询里正确传播。

`Transaction(ctx, fn)` 把事务封装成闭包。函数返回 nil 时提交，返回 error 时回滚，不需要调用方手工管理 `Begin / Commit / Rollback` 三步式操作。Context 在这里也有明确用途：`otelgorm` 会基于 Context 里的 span 继续向下挂载，事务内的多条 SQL 都会归属到同一个父 span 下。

`Close()` 是 no-op，原因在工程取舍一节说明。

配置结构非常小：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Driver` | `string` | `"mysql"` | 数据库驱动，支持 `mysql` / `postgresql` / `sqlite` |

`Driver` 是唯一的配置字段，DSN 由 `connector` 在建立连接时处理，`db` 不重复这部分工作。

选项通过函数式 `Option` 注入：

| 选项 | 说明 |
|------|------|
| `WithLogger(l)` | 注入 clog.Logger，SQL 日志通过适配器写入 |
| `WithTracer(tp)` | 注入 TracerProvider，注册 otelgorm 插件 |
| `WithMySQLConnector(c)` | Driver="mysql" 时必须 |
| `WithPostgreSQLConnector(c)` | Driver="postgresql" 时必须 |
| `WithSQLiteConnector(c)` | Driver="sqlite" 时必须 |
| `WithSilentMode()` | 禁用 SQL 日志，测试环境常用 |

---

## 4 核心概念与数据模型

`db` 组件只有两个核心概念值得单独说明：借用模型和 SQL 日志分级。

### 4.1 借用模型

`db` 不拥有底层连接，它从 `connector.GetClient()` 拿到 `*gorm.DB`，这个指针背后的连接池属于 `connector`。`db` 只在初始化时建立一个 `gorm.Session`，注入 logger 和 otelgorm 插件。连接池的创建、维护、健康检查和关闭，全部由 `connector` 负责。

这个模型的好处是所有权清晰：应用层只需要 `defer connector.Close()`，不需要同时追踪 `db` 的生命周期。

### 4.2 SQL 日志分级

`db` 实现了 GORM 的 `logger.Interface`，将 SQL 日志路由到 `clog`。分级策略如下：

- 执行出错：`error` 级别，消息为 `sql error`，附加 `duration`、`sql`、`rows`、`error` 字段
- 执行耗时超过 200ms：`warn` 级别，消息为 `slow sql`，附加同样字段
- 正常执行：`debug` 级别，消息为 `sql`

200ms 阈值是行业惯例，覆盖大多数生产环境慢查询场景。测试环境不需要这些日志时，使用 `WithSilentMode()` 将 GORM logger 设置为 Silent 级别即可关闭全部输出。

---

## 5 关键实现思路

`New(cfg, opts...)` 是整个组件的核心，它的初始化流水线如下：配置校验和默认值设置 → 选项应用 → 按 Driver 从 connector 取 `*gorm.DB` → 创建 Session 并注入 gormLogger → 可选注册 otelgorm → 返回 `database` 实例。

Session 的使用是一个值得注意的细节。GORM 的 `Session` 创建了一个新的数据库会话，可以在其上独立配置 Logger、DryRun 等选项，而不会影响原始的 `*gorm.DB`。这意味着 `db` 注入的日志配置不会污染 `connector` 共享的底层客户端，多个 `db` 实例可以共用同一个 `connector`，各自拥有独立的可观测性配置。

otelgorm 的注册时机也值得说明。在 `New()` 中，otelgorm 被注册为 GORM 插件（`gormDB.Use(plugin)`），它在 GORM 的 `Statement.Build` 阶段前后插入 span，捕获 SQL 语句、表名、执行耗时和错误信息。因为注册在 Session 上，不是全局插件，不会影响其他没有注入 Tracer 的 `db` 实例。

---

## 6 工程取舍与设计权衡

### 6.1 为什么 `DB(ctx)` 直接暴露 `*gorm.DB`

很多 ORM 封装层喜欢再包一层查询接口，例如 `Find(model interface{}, conds ...interface{}) error`，理由是"解耦 ORM 依赖"。但这条路走到底，要么接口越来越大（GORM 的能力实在很多），要么能力越来越弱（只能覆盖 80% 的场景，遇到 Pipeline、Raw SQL、Preload、关联写入时仍然要绕过封装）。

`db` 选择不做这层封装。`DB(ctx)` 直接返回 `*gorm.DB`，业务代码可以使用 GORM 的全部能力，不受封装层限制。这意味着上层代码确实依赖了 `gorm.io/gorm`，但在 Genesis 的架构定位里，`db` 本身就是 L1 基础设施层，它的职责本来就是管理 GORM 连接，而不是抽象 GORM。需要做数据库抽象的，是更上层的业务代码，而不是这一层。

换句话说：`db` 封装的是"如何建立和管理 GORM 连接"，而不是"如何使用 GORM 查询数据"。

### 6.2 为什么 `Close()` 是 no-op

`db` 组件里的 `Close()` 什么都不做。这不是设计遗漏，而是借用模型的必然结果。`connector` 拥有底层 `*gorm.DB`（实际上是连接池），`db` 只是拿来用，没有任何独占的资源需要释放。

如果 `db.Close()` 真的关闭了底层连接，在同一个 `connector` 被多个 `db` 实例共用时，一次 `Close()` 就会让其他实例的查询全部失败。所有权边界必须清晰：谁创建，谁负责关闭。`connector` 创建连接，`connector` 负责关闭。`db` 作为借用方，没有这个权利。

### 6.3 为什么不在 `db` 里提供分表能力

一个合理的问题是：既然 `db` 已经封装了 GORM 的初始化，为什么不顺便把分表中间件（`gorm.io/sharding`）也集成进来？这样用户只需配置规则，就能透明地使用分表。

这条路有几个根本性问题。第一，`gorm.io/sharding` 的内置 Snowflake ID 生成器没有暴露机器 ID 配置，在 Kubernetes 多副本部署时所有实例使用相同的 machine ID，必然产生 ID 碰撞。第二，分表方案需要手动创建物理表（`orders_0`、`orders_1`...），组件无法自动完成，稍有疏漏就是运行时 panic。第三，这类中间件只解决了表级分片（单数据库内多表），真正需要水平扩展时需要的是库级分片（多数据库实例），两者是完全不同的架构，一个 Config 字段根本表达不清楚。

更关键的是，PostgreSQL 和 MySQL 都原生支持声明式表分区（`PARTITION BY HASH / RANGE / LIST`），分区路由由数据库引擎完成，对应用层完全透明，不需要任何应用代码改动，索引、事务、外键全部正常工作。应用层中间件在这里是在用更脆弱的方式模拟数据库已经具备的能力。

`db` 的职责边界是连接管理和基础可观测性，不是分布式数据库策略。

### 6.4 为什么慢查询阈值固定为 200ms

200ms 是一个业界经验值，覆盖大多数在线服务对数据库延迟的接受上限。如果这个值需要调整，更合适的方式是在 GORM 层直接配置，而不是在 `db` 组件里暴露一个配置项。`db` 不是 DBA 工具，它的可观测性配置应当保持简单：用就用，不用就 `WithSilentMode`。暴露阈值配置会让组件的 Config 复杂化，而实际收益有限——真正需要精细慢查询治理的团队，往往会直接在数据库侧（慢查询日志、pg_stat_statements）做，而不是依赖应用层阈值告警。

---

## 7 适用场景与实践建议

`db` 适合以下场景：你需要一个标准化的 GORM 初始化方式，希望全项目统一 SQL 日志和 trace 接入，同时不想再封装一套查询接口。它特别适合微服务场景，每个服务用 `connector` + `db` 两层建立数据库访问，职责分明，初始化代码简洁。

它不适合以下场景：你需要完整的读写分离路由（需要多个 `connector` 和自定义路由逻辑）；你需要真正的水平分库（需要在 `db` 层之上再做 shard routing）；你的项目已经有成熟的 GORM 封装层，接入 `db` 反而增加间接层。

几条实践建议。

第一，不要再在 `db` 上包一层 DAO 接口。这是最常见的过度抽象。`db.DB(ctx)` 已经可以直接在服务层使用，DAO 封装应当针对业务模型，而不是针对 GORM 本身。

第二，生产环境同时注入 `WithLogger` 和 `WithTracer`，不要只注入其中一个。慢查询日志和 trace span 提供了互补的信息：日志适合聚合统计，trace 适合单次请求链路排查。

第三，测试环境优先使用 SQLite + `WithSilentMode`，避免测试日志噪音，同时享受 testcontainers 提供的零配置集成测试能力。SQLite 行为和 MySQL / PostgreSQL 在绝大多数场景下是兼容的，可以用来验证业务逻辑，驱动切换时无需改动测试代码。

第四，分表需求优先考虑数据库原生分区。在 PostgreSQL 上，`PARTITION BY HASH(user_id)` 加上若干分区表，可以在应用层零感知的情况下把大表数据分散到多个分区，配合分区索引，性能表现通常优于应用层中间件方案。

---

## 8 总结

`db` 的核心价值在于把"用 GORM 连接数据库"这件事做到标准化：统一初始化流程、自动接入可观测性、用借用模型明确资源所有权。它刻意保持薄封装，不重新包装 GORM 查询 API，也不试图解决分布式数据库策略这类属于更高层次的问题。

如果要用一句话总结 `db` 的设计原则，那就是：**封装初始化，不封装查询；管理可观测性，不管理数据策略。**
