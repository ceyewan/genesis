# Genesis 重构与审计计划 (Genesis Refactoring & Audit Plan)

> NOTE: 本文档为当前重构的执行计划（source-of-truth）。在进行代码重构或调整架构时，应以本文件的约定为优先。任何偏离本计划的重要决策，应记录在 `docs/reviews/architecture-decisions.md`。

**目标**：将 Genesis 从原型集合转变为生产级、符合 Go 习惯的微服务组件库。
**核心原则**：**显式优于隐式**（Explicit over Implicit）、**简单优于聪明**（Simple over Clever）、**组合优于继承**（Composition over Inheritance）。

---

## 1. 核心架构决策：移除 DI 容器

**已完成**：删除 `pkg/container` 及其相关逻辑。Genesis 现已转型为纯粹的组件库。

| 决策 | 描述 | 状态 |
| :----- | :----- | :----- |
| **移除 Container** | 删除所有 DI 容器逻辑，改为 Go Native 初始化 | [x] 已完成 |
| **显式注入** | 依赖通过构造函数或 Option 显式传入 | [x] 已完成 |
| **资源所有权** | 明确 "谁创建，谁负责释放" 的原则 | [x] 已完成 |

---

## 2. 总体架构：四层模型 (Four-Layer Model)

Genesis 简化为四层扁平化结构：

| 层次 | 核心组件 | 职责 |
| :----- | :--------- | :----- |
| **Level 3: Governance** | `auth`, `ratelimit`, `breaker`, `registry` | 流量治理，身份认证 |
| **Level 2: Business** | `cache`, `idgen`, `dlock`, `idempotency`, `mq` | 业务能力封装 |
| **Level 1: Infrastructure** | `connector`, `db` | 连接管理，底层 I/O |
| **Level 0: Base** | `clog`, `config`, `metrics`, `xerrors` | 框架基石 |

---

## 3. 重构执行清单

### 3.1 阶段 1：核心基座与连接器 (Completed)

- [x] **clog**: 基于 slog 的重构，支持 Context 和 Namespace。
- [x] **config**: 支持多源加载和强类型绑定。
- [x] **connector**: 统一 MySQL, Redis, Etcd 连接管理，移除 Lifecycle 依赖。
- [x] **xerrors**: 建立统一的错误处理机制。

### 3.2 阶段 2：业务组件扁平化 (In Progress)

- [x] **dlock**: 扁平化重构，支持 Redis/Etcd 驱动。
- [x] **idgen**: 支持 Snowflake/UUID。
- [x] **cache**: 统一缓存接口。
- [x] **mq**: 统一消息队列接口。
- [x] **idempotency**: 幂等插件重构。

### 3.3 阶段 3：治理组件增强 (Pending)

- [-] **auth**: JWT 与中间件重构。
- [ ] **ratelimit**: 扁平化重构与适配器。
- [ ] **breaker**: 适配器模式重构。
- [ ] **registry**: 服务发现接口对齐。

---

## 4. 关键技术规范

### 4.1 资源释放 (Defer LIFO)

应用启动时，利用 `defer` 的后进先出特性，实现资源的正确关闭：

```go
redisConn := connector.MustNewRedis(...)
defer redisConn.Close()

mysqlConn := connector.MustNewMySQL(...)
defer mysqlConn.Close()
```

### 4.2 扁平化代码组织 (Flattening)

L2/L3 组件不再使用 `types/` 子包，所有导出类型置于包根目录。

---

## 5. 执行路线图 (Updated)

| 阶段 | 任务 | 目标 | 状态 |
| :----- | :----- | :----- | :----- |
| **Phase 1** | **基座固化** | 确定 L0/L1 规范，删除 Container | 完成 |
| **Phase 2** | **L2 扁平化** | 重构 cache, dlock, idgen 为扁平结构 | 进行中 |
| **Phase 3** | **L3 治理** | 完成 auth, ratelimit, breaker 重构 | 待启动 |
| **Phase 4** | **文档同步** | 更新所有 README 和 Design docs | 进行中 |
| **Phase 5** | **示例迁移** | 更新 examples 目录，展示 Go Native DI | 待启动 |
