# Genesis

> 一个轻量级、标准化、高可扩展的 Go 微服务基座库。

Genesis 旨在为 Go 微服务开发提供一套**统一的架构规范**和**开箱即用的组件集合**。它通过分层设计和依赖注入，帮助开发者快速构建健壮、可维护的微服务应用。

## ✨ 核心特性

* **标准化日志 (clog):** 基于 `slog`，支持 Context 字段自动提取、多级命名空间。
* **统一配置中心 (config):** 通过 `pkg/config` 将本地文件、环境变量和远程配置中心汇总为强类型 `AppConfig`，支持热更新。
* **统一连接管理 (connector):** 统一管理 MySQL, Redis, Etcd 等基础设施连接，支持复用和健康检查。
* **可观测性 (telemetry):** 基于 OpenTelemetry 的 Metrics & Tracing，与 clog 深度集成，支持全链路观测。
* **生命周期管理 (container):** 极简的 DI 容器，确保组件有序启动和优雅停机。
* **增强型 DB 组件:** 基于 GORM，无缝集成 `sharding` 分库分表，提供统一事务接口。
* **分布式锁 (dlock):** 统一接口，支持 Redis/Etcd 后端，内置自动续期 (Watchdog)。

## 📚 文档

* [架构设计 (Architecture)](docs/genesis-design.md)
* [组件开发规范 (Component Spec)](docs/specs/component-spec.md)
* [容器设计 (Container)](docs/container-design.md)
* [配置中心设计 (Config)](docs/config-design.md)
* [可观测性设计 (Telemetry)](docs/telemetry-design.md)
* [日志库设计 (Clog)](docs/clog-design.md)
* [连接器设计 (Connector)](docs/connector-design.md)
* [数据库组件设计 (DB)](docs/db-design.md)
* [分布式锁设计 (DLock)](docs/dlock-design.md)

## 🚀 快速开始

```go
package main

import (
    "context"
    "genesis/pkg/clog"
    "genesis/pkg/config"
    "genesis/pkg/container"
)

func main() {
    ctx := context.Background()

    // 1. 使用 config.Manager 加载应用配置并绑定到 AppConfig
    mgr := config.NewManager(config.WithPaths("./config"))
    if err := mgr.Load(ctx); err != nil {
        panic(err)
    }
    var appCfg AppConfig
    if err := mgr.Unmarshal(&appCfg); err != nil {
        panic(err)
    }

    // 2. 初始化应用级 Logger（附加 app namespace）
    logger := clog.New(appCfg.Log).WithNamespace(appCfg.App.Namespace)

    // 3. 初始化容器（统一管理连接器与组件的生命周期）
    app, err := container.New(appCfg, container.WithLogger(logger), container.WithConfigManager(mgr))
    if err != nil {
        panic(err)
    }
    defer app.Close() // 优雅停机

    // 4. 使用组件
    app.Log.InfoContext(ctx, "service started")

    // 使用 DB
    var user User
    app.DB.DB(ctx).First(&user, 1)

    // 使用分布式锁
    if err := app.DLock.Lock(ctx, "resource_key"); err == nil {
        defer app.DLock.Unlock(ctx, "resource_key")
        // 业务逻辑...
    }
}
```

## 🗺️ 路线图 (Roadmap)

* [x] **Core:** Log, Config, Telemetry, Container, Connector
* [x] **Storage:** DB (Sharding), DLock
* [ ] **Middleware:** Cache, MQ, ID Gen
* [ ] **Governance:** Rate Limit, Idempotency, Registry, Circuit Breaker

## 📄 License

MIT
