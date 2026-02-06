# Genesis db：基于 GORM 的数据库组件设计与实现

Genesis `db` 是基础设施层（L1）组件，在 `connector` 提供的连接能力之上，统一封装了 GORM 使用、事务管理、分库分表插件接入以及可观测性集成。它的目标是“保留 GORM 原生体验”，同时把跨项目重复出现的基础配置与治理逻辑沉淀为标准组件。

---

## 0. 摘要

- `db` 对外暴露极简 `DB` 接口：`DB(ctx)`、`Transaction(ctx, fn)`、`Close()`。
- 通过 `Config.Driver` 实现配置驱动，支持 `mysql`、`postgresql`、`sqlite`。
- 连接器采用显式注入：不同驱动必须注入对应 Connector，否则初始化失败。
- 分片能力基于 `gorm.io/sharding` 插件，支持按分片键路由到逻辑表后缀。
- 可观测性默认接入 `clog`（SQL 日志）与 OpenTelemetry（`otelgorm` span）。
- 资源所有权遵循借用模型：`db.Close()` 为 no-op，底层连接由 Connector 关闭。

---

## 1. 背景：为什么需要 db 组件

业务团队直接使用 GORM 往往会遇到这些问题：

- 初始化方式不一致：不同数据库驱动创建流程不同。
- 可观测性分散：SQL 日志、trace 插件注册、慢查询阈值常被各服务重复实现。
- 分片接入成本高：每个服务都要自行注册 sharding 插件并维护规则。
- 生命周期不清晰：谁负责关闭底层连接容易混乱。

`db` 组件的设计重点不是重新包装 ORM API，而是统一初始化与治理能力，让业务代码继续使用熟悉的 `*gorm.DB`。

---

## 2. 核心设计：薄封装 + 显式依赖

### 2.1 极简接口

`db` 组件的接口刻意保持很薄：

- `DB(ctx)`：返回带 `context` 的 `*gorm.DB`，业务继续写原生 GORM 查询。
- `Transaction(ctx, fn)`：提供闭包式事务边界，统一提交/回滚语义。
- `Close()`：组件本身不拥有连接资源，保持 no-op。

这种设计避免“再造 ORM 抽象层”，降低学习与迁移成本。

### 2.2 配置驱动与校验

核心配置：

- `driver`: `mysql | postgresql | sqlite`（默认 `mysql`）
- `enable_sharding`: 是否启用分片
- `sharding_rules`: 分片规则列表

校验规则：

- 驱动必须在支持列表内。
- 开启分片时必须提供规则。
- 每条规则必须包含非空 `sharding_key`、正数 `number_of_shards`、非空 `tables`。

### 2.3 连接器注入策略

初始化时按驱动读取对应 connector：

- `mysql` -> `WithMySQLConnector`
- `postgresql` -> `WithPostgreSQLConnector`
- `sqlite` -> `WithSQLiteConnector`

如果缺失，会返回明确错误（如 `db: mysql connector is required`），避免“运行后才报错”。

---

## 3. 初始化流水线：New 做了什么

`New(cfg, opts...)` 的关键流程：

1. 处理配置默认值并校验。
2. 应用函数式选项（logger、tracer、connector、silentMode）。
3. 根据 driver 选择已注入的 `*gorm.DB` 客户端。
4. 注入 GORM logger 适配器，将 SQL 日志统一写入 `clog`。
5. （可选）注册 `otelgorm` 插件，自动生成数据库 trace span。
6. （可选）注册 sharding 插件，按规则对指定逻辑表启用分片路由。

整个过程体现了 Genesis 的“显式优于隐式”：依赖、能力开关、行为边界都在构造阶段明确。

---

## 4. 事务模型与上下文传播

`Transaction(ctx, fn)` 直接委托给 GORM 事务：

- `fn` 返回 `nil` -> 提交事务
- `fn` 返回 error -> 回滚事务

组件会把 `ctx` 透传到 `WithContext`，保证日志、trace、超时控制在事务内一致生效。

这比业务手工 `Begin/Commit/Rollback` 更稳定，也更容易写出可测试的事务代码。

---

## 5. 可观测性设计

### 5.1 SQL 日志接入 clog

`db/gorm_logger.go` 对 GORM logger 进行了适配：

- SQL 执行错误：`error` 级别，消息 `sql error`
- 慢查询（>200ms）：`warn` 级别，消息 `slow sql`
- 普通 SQL：`debug` 级别，消息 `sql`

并附加关键字段：`duration`、`sql`、`rows`。

### 5.2 静默模式

`WithSilentMode()` 可关闭 SQL 日志输出，适合测试或无需 SQL 日志的环境，避免噪音和 I/O 开销。

### 5.3 OpenTelemetry trace

注入 `WithTracer(tp)` 后，组件会注册 `otelgorm` 插件，为数据库调用自动创建 span，减少手工埋点工作量。

---

## 6. 分库分表能力：基于 gorm.io/sharding

### 6.1 基本原理

`db` 组件在初始化时按 `ShardingRule` 注册 sharding 中间件：

- 指定 `ShardingKey`（如 `user_id`）
- 指定 `NumberOfShards`（如 64）
- 指定作用表集合（如 `orders`）

插件会在执行 SQL 时解析条件并路由到物理表（如 `orders_0` ~ `orders_63`）。

### 6.2 当前实现特征

- 主键生成器配置为 `sharding.PKSnowflake`。
- 每条规则独立注册，支持多个逻辑表组。
- 业务层仍使用逻辑表名，路由细节由插件处理。

### 6.3 使用注意事项

- 分片表必须预先创建好对应后缀表。
- 查询/更新/删除应携带分片键条件，否则可能路由失败或触发全路由代价。
- 分片规则属于“架构级配置”，变更需要谨慎评估历史数据迁移。

---

## 7. 生命周期与资源所有权

`db` 组件采用 Borrowing Model：

- Connector 是 Owner：负责底层连接创建与关闭。
- `db` 是 Borrower：借用 `*gorm.DB` 客户端，不管理连接生命周期。

因此推荐关闭顺序：

1. `database.Close()`（可选，no-op）
2. `connector.Close()`（必须）

实际工程里通常只需要保证 connector 在应用退出前被 `defer Close()`。

---

## 8. 实践建议

- 默认把 `db` 当“GORM 启动器 + 治理层”来用，不要再包一层 DAO 框架。
- 对强一致业务，事务边界统一走 `Transaction(ctx, fn)`。
- 生产环境启用 trace，开发环境按需开启 SQL 日志；压测/测试可用 `WithSilentMode`。
- 分片先从单表、低分片数验证，再扩展到多表规则，避免一次性复杂配置。

---

## 9. 设计取舍总结

`db` 的核心价值是三点：

- 对业务保持 GORM 原生开发体验；
- 对基础设施统一连接器注入、日志、追踪和分片治理；
- 对生命周期和错误语义给出清晰边界。

这让它既足够轻量，又能覆盖微服务数据库访问中的关键工程问题。
