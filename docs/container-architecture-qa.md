# 容器架构设计问答与决策记录

本文档记录了在开发 `genesis` 容器和连接器过程中，关于架构设计、依赖注入和生命周期管理的关键讨论和决策。

## 1. 连接器生命周期管理

### 问题

连接器接口（如 `MySQLConnector`）未实现 `Lifecycle` 接口，导致无法注册到 `LifecycleManager` 中。

### 决策

让所有连接器接口显式继承 `Lifecycle` 接口，并在实现类中添加 `Start()`、`Stop()`、`Phase()` 方法，以确保编译时类型安全和统一的生命周期管理。

## 2. 容器的定位与心智负担

### 问题

在简单场景（如仅使用数据库和单机限流）下，是否必须使用 `container.New()`？这是否会增加用户的心智负担？

### 决策

容器 (`Container`) 的使用取决于组件类型：

| 组件类型 | 示例 | 是否必须通过容器启动？ | 理由 |
| :--- | :--- | :--- | :--- |
| **基础设施** | MySQL, Redis, Etcd | 是 | 容器提供生产级的生命周期管理、连接池和优雅关闭。 |
| **工具库** | 单机限流, clog | 否 | 无外部依赖，用户可独立使用，框架不强制限制。 |

结论：容器主要用于封装复杂的**生产级健壮性**逻辑，其带来的初始化开销是值得的。

## 3. 横切关注点（Logger/Metrics）的注入

### 问题

`Logger` 和 `Metrics` 等横切关注点应该如何集成？它们是否应该被注入，而不是由连接器内部创建？

### 决策

`Logger` 和 `Metrics` 必须作为核心依赖，在容器启动的**最早阶段**初始化，并通过**依赖注入**的方式传递给所有连接器。

**注入机制：**

1. `Container` 结构体持有 `clog.Logger` 和 `Metrics` 接口实例。
2. 在 `Container.initManagers()` 中，通过闭包捕获的方式，将这些依赖注入到连接器的工厂函数中。

## 4. MySQL 日志的自定义命名空间

### 问题

当前的 GORM 日志无法自定义命名空间，且未集成 `clog`，导致所有数据库日志硬编码或使用容器默认命名空间（如 `container`）。用户希望能够指定如 `user-service.db` 的命名空间。

### 解决方案（设计）

1. **配置扩展**：在 `MySQLConfig` 中增加 `LogNamespace` 字段。
2. **GORM 适配器**：创建 GORM Logger 适配器，将 GORM 日志重定向到 `clog.Logger`。
3. **注入细分**：连接器工厂函数接收容器的根 Logger，并根据 `LogNamespace` 创建一个子 Logger (`rootLog.Namespace(config.LogNamespace)`)，然后将子 Logger 传递给 GORM 适配器。

## 5. 复杂依赖链与多阶段初始化

### 问题

如何处理复杂的依赖链，例如配置中心依赖 Etcd，而其他基础设施又依赖配置中心 (`logger -> etcdClient -> ConfigCenter -> mysql/redis`)？

### 决策：多阶段初始化 (Phased Initialization)

`Container.New()` 必须重构为多阶段启动，以确保依赖按顺序初始化：

| 阶段 | 目标 | 依赖 |
| :--- | :--- | :--- |
| **Phase 1** | 核心工具初始化 | 无 |
| **Phase 2** | 配置中心连接 | Etcd 配置 |
| **Phase 3** | 动态配置加载与基础设施连接 | `ConfigCenter` |
| **Phase 4** | 高级组件初始化 | 连接器实例 |

此流程确保了 Etcd 连接器在配置中心之前启动，配置中心在依赖它的连接器之前加载配置。
