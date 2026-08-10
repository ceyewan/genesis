# Genesis

> 一个面向 Go 微服务的轻量组件库，而不是框架。

当前已发布预发行基线为
[`v1.0.0-rc.1`](https://github.com/ceyewan/genesis/releases/tag/v1.0.0-rc.1)，
tag 指向 `ec5ad2c31fb4adce2bd42529e3d7fbfe92b23aa7`。后续测试和文档提交不会
自动产生新版本；生产修复需要独立的 `rc.2` 决策与完整发布门禁。

Genesis 提供一组可以直接组合的基础设施与治理组件，目标不是接管应用，而是把日志、配置、连接管理、缓存、分布式锁、消息、认证、限流、熔断、注册发现等通用能力沉淀成统一积木。

最低支持 Go `1.26.0`。CI 同时使用最低版本和当前固定补丁版本
`1.26.5` 验证编译、测试与发布证据；消费者不应依赖本机更高版本才能通过。

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

    loader, err := config.New(nil)
    if err != nil { panic(err) }
    defer loader.Close()
    if err := loader.Load(ctx); err != nil { panic(err) }

    var cfg AppConfig
    if err := loader.Unmarshal(&cfg); err != nil { panic(err) }
    logger, err := clog.New(&cfg.Log)
    if err != nil { panic(err) }
    defer logger.Close()

    redisConn, err := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    if err != nil { panic(err) }
    defer redisConn.Close()
    if err := redisConn.Connect(ctx); err != nil { panic(err) }

    cacheClient, err := cache.NewDistributed(&cfg.Cache, cache.WithRedisConnector(redisConn), cache.WithLogger(logger))
    if err != nil { panic(err) }
    defer cacheClient.Close()

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
make api-inventory-check
make api-compat-check

# 文档
go doc -all ./<component>

# 示例
make examples
make example-<component>
```

## 测试约束

- 仓库内测试优先使用 `internal/testkit` 提供的容器化 helper，例如
  `testkit.NewRedisContainerClient(t)`、`testkit.NewMySQLDB(t)`。外部项目维护自己的 fixture。
- 集成测试通过 `testcontainers` 自动拉起依赖，不要在测试前手动执行 `make up`。
- 测试断言使用 `require`，不要新增 `assert`。

## 文档入口

- [总体设计](docs/genesis-design.md)
- [组件文档审计规范](docs/component-doc-audit-guide.md)
- [组件设计文档索引](docs/README.md)
- [示例索引](examples/README.md)
- [仓库内部测试指南](internal/testkit/README.md)
- [v1.0.0-rc.1 契约加固与消费者风险清单](docs/v1-rc1-contract-hardening.md)
- [v1 后端与工具链兼容矩阵](docs/v1-compatibility.md)
- [v1 安全与依赖治理政策](docs/v1-security-and-dependencies.md)

## License

MIT
