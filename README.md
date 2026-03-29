# Genesis

> 一个面向 Go 微服务的轻量组件库，而不是框架。

Genesis 提供一组可以直接组合的基础设施与治理组件，目标不是接管应用，而是把日志、配置、连接管理、缓存、分布式锁、消息、认证、限流、熔断、注册发现等通用能力沉淀成统一积木。

项目的核心约束只有三条：
- 显式依赖注入，不使用运行时 DI 容器。
- 组件边界清楚，能力按层组织但包结构保持扁平。
- 谁创建，谁 `Close()`；连接器拥有资源，业务组件只借用资源。

## 架构分层

| 层次 | 核心组件 | 职责 |
| :--- | :--- | :--- |
| **Level 3: Governance** | `auth`, `ratelimit`, `breaker`, `registry` | 认证与流量治理 |
| **Level 2: Business** | `cache`, `idgen`, `dlock`, `idem`, `mq` | 业务通用能力 |
| **Level 1: Infrastructure** | `connector`, `db` | 连接管理与数据库访问 |
| **Level 0: Base** | `clog`, `config`, `metrics`, `trace`, `xerrors` | 基础能力与统一约束 |

## 项目状态

当前分支已经系统收敛并重写了所有核心组件的实现边界与文档，重点包括：
- `auth` 已切换为双 JWT 令牌模型。
- `idgen` 已收紧 snowflake 位布局、allocator 所有权和 sequencer 语义。
- `ratelimit` 已收紧分布式 key 设计、Redis 时间语义与错误策略。
- `dlock` 已收紧锁生命周期、TTL 校验与 `Close()` 语义。
- `registry` 已收紧 gRPC-only endpoint、resolver 空状态、watch 恢复语义与 `Close()` 返回值。
- `breaker` 已补 gRPC 错误分类、统一拒绝错误模型与配置校验。
- `idem` 已收紧返回值稳定性、缓存策略和锁续期语义。

## 快速开始

```go
package main

import (
    "context"
    "os/signal"
    "syscall"

    "github.com/ceyewan/genesis/cache"
    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/config"
    "github.com/ceyewan/genesis/connector"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    cfg, _ := config.Load("config.yaml")
    logger, _ := clog.New(&cfg.Log)
    defer logger.Close()

    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()

    cacheClient, _ := cache.New(&cfg.Cache, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))

    _, _ = cacheClient.Get(ctx, "demo:key")
}
```

更完整的总体设计见 [docs/genesis-design.md](docs/genesis-design.md)。各组件的定位、边界、接入方式和设计取舍见 [docs/README.md](docs/README.md)。

## 常用命令

```bash
# 代码质量
go test ./...
go test -race -count=1 ./...
make lint
make modernize
make modernize-check

# 文档
go doc -all ./<component>

# 示例
make examples
make example-<component>
```

## 测试约束

- 优先使用 `testkit` 提供的容器化 helper，例如 `testkit.NewRedisContainerClient(t)`、`testkit.NewMySQLDB(t)`。
- 集成测试通过 `testcontainers` 自动拉起依赖，不要在测试前手动执行 `make up`。
- 测试断言使用 `require`，不要新增 `assert`。

## 文档入口

- [总体设计](docs/genesis-design.md)
- [组件文档审计规范](docs/component-doc-audit-guide.md)
- [组件设计文档索引](docs/README.md)
- [示例索引](examples/README.md)
- [测试指南](testkit/README.md)

## License

MIT
