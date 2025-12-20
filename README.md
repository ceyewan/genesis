# Genesis

> 一个轻量级、标准化、高可扩展的 Go 微服务组件库。

Genesis 旨在为 Go 微服务开发提供一套**统一的架构规范**和**开箱即用的组件集合**。它通过显式依赖注入和扁平化设计，帮助开发者快速构建健壮、可维护的微服务应用。

**Genesis 不是框架**——我们提供积木，用户自己搭建。

## ✨ 核心特性

* **标准化日志 (clog):** 基于 `slog`，支持 Context 字段自动提取、多级命名空间派生。
* **统一配置 (config):** 强类型配置管理，支持多源加载。
* **显式连接管理 (connector):** 统一管理 MySQL, Redis, Etcd, NATS 等基础设施连接。
* **可观测性 (metrics):** 基于 OpenTelemetry 的指标收集，支持自动埋点。
* **Go Native DI:** 弃用 DI 容器，拥抱原生的构造函数注入，依赖关系一目了然。
* **增强型 DB 组件:** 基于 GORM，集成 `sharding` 分库分表。
* **分布式锁 (dlock):** 统一接口，支持 Redis/Etcd 后端，内置自动续期。

## 📚 文档

* [架构设计 (Architecture)](docs/genesis-design.md)
* [重构计划 (Refactoring Plan)](docs/refactoring-plan.md)
* [组件开发规范 (Component Spec)](docs/specs/component-spec.md)
* [配置中心设计 (Config)](docs/foundation/config-design.md)
* [日志库设计 (Clog)](docs/foundation/clog-design.md)
* [连接器设计 (Connector)](docs/infrastructure/connector-design.md)
* [分布式锁设计 (DLock)](docs/business/dlock-design.md)

## 🚀 快速开始

```go
package main

import (
    "context"
    "os/signal"
    "syscall"

    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/config"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/db"
    "github.com/ceyewan/genesis/pkg/dlock"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // 1. 加载配置
    cfg, _ := config.Load("config.yaml")

    // 2. 初始化 Logger
    logger, _ := clog.New(&cfg.Log)

    // 3. 创建连接器 (defer 自动释放资源)
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()

    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
    defer mysqlConn.Close()

    // 4. 初始化组件 (显式注入依赖)
    database, _ := db.New(mysqlConn, &cfg.DB, db.WithLogger(logger))
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock, dlock.WithLogger(logger))

    // 5. 使用组件
    logger.InfoContext(ctx, "service started")
    
    var user struct{ ID int64 }
    database.DB(ctx).First(&user, 1)

    if err := locker.Lock(ctx, "my_resource"); err == nil {
        defer locker.Unlock(ctx, "my_resource")
        // do business logic...
    }
}
```

## 🗺️ 路线图 (Roadmap)

* [x] **Base (L0):** Log, Config, Metrics, XErrors
* [x] **Infra (L1):** Connector, DB
* [x] **Business (L2):** DLock, Cache, MQ, IDGen, Idempotency
* [ ] **Governance (L3):** Auth (Refactoring), Rate Limit, Circuit Breaker, Registry

## 📄 License

MIT
