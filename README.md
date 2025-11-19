# Genesis

> 一个轻量级、标准化、高可扩展的 Go 微服务基座库。

Genesis 旨在为 Go 微服务开发提供一套**统一的架构规范**和**开箱即用的组件集合**。它通过分层设计和依赖注入，帮助开发者快速构建健壮、可维护的微服务应用。

## ✨ 核心特性

* **标准化日志 (clog):** 基于 `slog`，支持 Context 字段自动提取、多级命名空间。
* **统一连接管理 (Connector):** 统一管理 MySQL, Redis, Etcd 等基础设施连接，支持复用和健康检查。
* **生命周期管理 (Container):** 极简的 DI 容器，确保组件有序启动和优雅停机。
* **增强型 DB 组件:** 基于 GORM，无缝集成 `sharding` 分库分表，提供统一事务接口。
* **分布式锁 (DLock):** 统一接口，支持 Redis/Etcd 后端，内置自动续期 (Watchdog)。

## 📚 文档

* [架构设计 (Architecture)](docs/genesis-design.md)
* [日志库设计 (Clog)](docs/clog-design.md)
* [连接器设计 (Connector)](docs/connector-design.md)
* [数据库组件设计 (DB)](docs/db-design.md)
* [分布式锁设计 (DLock)](docs/dlock-design.md)

## 🚀 快速开始

```go
package main

import (
    "context"
    "genesis/pkg/container"
    "genesis/pkg/clog"
)

func main() {
    // 1. 初始化容器 (加载配置、连接器、组件)
    app, err := container.New(config)
    if err != nil {
        panic(err)
    }
    defer app.Close() // 优雅停机

    // 2. 使用组件
    ctx := context.Background()
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

* [x] **Core:** Log, Container, Connector
* [x] **Storage:** DB (Sharding), DLock
* [ ] **Middleware:** Cache, MQ, ID Gen, Metrics
* [ ] **Governance:** Rate Limit, Idempotency, Registry, Config, Circuit Breaker

## 📄 License

MIT
