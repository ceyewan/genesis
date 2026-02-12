# Genesis DB：基于 GORM 的数据库组件设计与实现

Genesis `db` 是基础设施层（L1）组件，在连接器提供的连接能力之上，统一封装了 GORM 使用、事务管理、分库分表插件接入以及可观测性集成。它的目标是保留 GORM 原生体验，同时把跨项目重复出现的基础配置与治理逻辑沉淀为标准组件。

---

## 0 摘要

- `db` 对外暴露极简 DB 接口：DB(ctx)、Transaction(ctx, fn)、Close()
- 通过 Config.Driver 实现配置驱动，支持 mysql、postgresql、sqlite
- 连接器采用显式注入：不同驱动必须注入对应 Connector，否则初始化失败
- 分片能力基于 gorm.io/sharding 插件，支持按分片键路由到逻辑表后缀
- 可观测性默认接入 clog（SQL 日志）与 OpenTelemetry（otelgorm span）
- 资源所有权遵循借用模型：db.Close() 为 no-op，底层连接由 Connector 关闭

---

## 1 背景：为什么需要 db 组件

业务团队直接使用 GORM 往往会遇到初始化方式不一致的问题，不同数据库驱动创建流程不同。可观测性分散，SQL 日志、trace 插件注册、慢查询阈值常被各服务重复实现。分片接入成本高，每个服务都要自行注册 sharding 插件并维护规则。生命周期不清晰，谁负责关闭底层连接容易混乱。

`db` 组件的设计重点不是重新包装 ORM API，而是统一初始化与治理能力，让业务代码继续使用熟悉的 `*gorm.DB`。

---

## 2 核心设计：薄封装与显式依赖

### 2.1 极简接口

`db` 组件的接口刻意保持很薄：DB(ctx) 返回带 context 的 `*gorm.DB`，业务继续写原生 GORM 查询；Transaction(ctx, fn) 提供闭包式事务边界，统一提交或回滚语义；Close() 为 no-op。这种设计避免再造 ORM 抽象层，降低学习与迁移成本。

### 2.2 配置驱动与校验

核心配置包括 driver（mysql、postgresql、sqlite）用于选择数据库驱动，enable_sharding 和 sharding_rules 用于配置分片规则。校验规则要求驱动必须在支持列表内，开启分片时必须提供规则。每条规则必须包含非空 sharding_key、正数 number_of_shards、非空 tables。

### 2.3 连接器注入策略

初始化时按驱动读取对应 connector。MySQL 使用 WithMySQLConnector，PostgreSQL 使用 WithPostgreSQLConnector，SQLite 使用 WithSQLiteConnector。如果缺失会返回明确错误，避免运行后才报错。

---

## 3 初始化流水线：New 做了什么

`New(cfg, opts...)` 的关键流程如下：

1.  **配置处理**：处理配置默认值并校验。
2.  **选项应用**：应用函数式选项如 logger、tracer、connector、silentMode。
3.  **客户端选择**：根据 driver 选择已注入的 `*gorm.DB` 客户端。
4.  **日志适配**：注入 GORM logger 适配器将 SQL 日志统一写入 clog。
5.  **追踪注册**：可选注册 otelgorm 插件自动生成数据库 trace span。
6.  **分片注册**：可选注册 sharding 插件按规则对指定逻辑表启用分片路由。

整个过程体现了 Genesis 的显式优于隐式原则，依赖、能力开关、行为边界都在构造阶段明确。

---

## 4 事务模型与上下文传播

`Transaction(ctx, fn)` 直接委托给 GORM 事务。函数返回 nil 时提交事务，返回 error 时回滚事务。组件会把 ctx 透传到 WithContext，保证日志、trace、超时控制在事务内一致生效。这比业务手工 Begin、Commit、Rollback 更稳定，也更容易写出可测试的事务代码。

---

## 5 可观测性设计

### 5.1 SQL 日志接入 clog

`db/gorm_logger.go` 对 GORM logger 进行了适配。SQL 执行错误时使用 error 级别，消息为 sql error。慢查询超过 200 毫秒时使用 warn 级别，消息为 slow sql。普通 SQL 使用 debug 级别。并附加关键字段 duration、sql、rows。

### 5.2 静默模式

WithSilentMode() 可关闭 SQL 日志输出，适合测试或无需 SQL 日志的环境，避免噪音和 I/O 开销。

### 5.3 OpenTelemetry trace

注入 WithTracer(tp) 后，组件会注册 otelgorm 插件，为数据库调用自动创建 span，减少手工埋点工作量。

---

## 6 分库分表能力：基于 gorm.io/sharding

### 6.1 什么是分库分表

分库分表是解决单表数据量瓶颈的经典方案。当单表数据量超过千万级时，查询性能会显著下降，索引维护成本变高。通过将数据分散到多个物理数据库（分片），可以线性扩展读写能力。

### 6.2 为什么 db 组件选择分表而非分库

Genesis 选择在应用层实现分表，而非中间件模式，主要基于以下考量：

1.  **简单性**：分表通过 ShardingKey 直接路由到目标物理表，不需要额外引入分库中间件。分库中间件通常需要维护复杂的路由规则、跨分片查询、结果合并等逻辑，增加系统复杂度。
2.  **透明性**：使用分表时，业务代码仍然直接操作 GORM 的 DB 接口，查询和写入的语义与单表完全一致。分库中间件为了实现路由透明，往往需要限制 SQL 语法，这会损失 SQL 的灵活性。
3.  **轻量级**：分表实现基于标准的 `gorm.io/sharding` 插件，这是一个标准的 GORM 扩展，无需引入额外组件。分库中间件则需要额外的网络服务和维护成本。
4.  **完整性**：分库中间件为了实现路由透明，往往会封装或改变 SQL 执行方式，增加学习成本和调试难度。分表方案让业务代码继续使用原生 GORM 查询，不受中间件限制。

### 6.3 分表路由原理

分表路由的核心思想是将数据按分片键分散到不同的物理表。假设按 user_id 分 64 个分片，分片键为 `user_id % 64`。当执行查询时，通过提取分片键计算目标表后缀，如 `users_31`，直接路由到对应的物理表。

`gorm.io/sharding` 插件会在 SQL 执行阶段自动解析查询中的分片键。例如查询 `SELECT * FROM users WHERE user_id = ?`，插件会解析 WHERE 条件中的 `user_id`，将 `users` 替换为配置的表名数组中的表名。如果配置了 64 个分片，插件会按顺序尝试每个表，找到实际存储数据的表后执行查询。

### 6.4 主键生成与分布

分表需要保证全局唯一的主键。db 组件使用 `sharding.PKSnowflake` 作为主键生成器，这是一个基于雪花算法的分布式 ID 生成器，能够生成全局唯一的 64 位整数 ID。ID 的高位通常配置为时间戳或机器 ID，确保不同分片生成的 ID 不会冲突。

### 6.5 查询模式与注意事项

使用分表后，查询仍然通过原生 GORM 接口完成。但需要注意几点：
- 写入数据时必须携带正确的分片键值，否则可能路由到错误的分片。
- 跨分片查询需要在应用层聚合结果。
- 批量操作应按分片分组，避免跨分片事务。

---

## 7 生命周期与资源所有权

`db` 组件采用借用模型：
- **Owner**：Connector 负责底层连接创建与关闭。
- **Borrower**：db 借用 `*gorm.DB` 客户端，不管理连接生命周期。

推荐关闭顺序是 `database.Close()`（可选，no-op）和 `connector.Close()`（必须）。实际工程里通常只需要保证 connector 在应用退出前被 defer Close()。

---

## 8 实践建议

- 默认把 db 当 GORM 启动器加治理层来用，不要再包一层 DAO 架架。
- 对强一致业务，事务边界统一走 `Transaction(ctx, fn)`。
- 生产环境启用 trace，开发环境按需开启 SQL 日志。
- 压测或测试可用 `WithSilentMode`。

---

## 9 设计取舍总结

`db` 的核心价值在于三个方面：
1.  **原生体验**：对业务保持 GORM 原生开发体验。
2.  **统一治理**：对基础设施统一连接器注入、日志、追踪和分片治理。
3.  **清晰边界**：对生命周期和错误语义给出清晰边界。

这让它既足够轻量，又能覆盖微服务数据库访问中的关键工程问题。
