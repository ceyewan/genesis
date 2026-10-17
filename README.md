# Genesis

> 一个面向 Go 微服务的轻量组件库，而不是框架。

Genesis 将日志、配置、连接管理、缓存、分布式锁、消息、认证、限流、熔断和
服务发现等通用能力沉淀成可以按需组合的 Go 包。它不接管应用生命周期，也不要求
使用依赖注入容器。

最低支持 Go `1.26.0`。当前预发行版本为
[`v1.0.0-rc.3`](https://github.com/ceyewan/genesis/releases/tag/v1.0.0-rc.3)，
发布提交为 `285d5647f903dcdb4f4d2b71f138624f5db8da64`。
本版本包含可观测性与 JetStream durable 启动修复，仍不是稳定版。
CI 通过不会自动产生新版本；后续状态以 GitHub Releases 为准。

## 设计原则

- 显式依赖注入，不使用运行时 DI 容器。
- 组件按能力分层，代码目录保持扁平。
- 谁创建资源，谁负责关闭。
- 统一入口不等于抹平后端差异。

## 架构分层

| 层次 | 组件 | 说明 |
| :--- | :--- | :--- |
| 基础能力 | `clog`、`config`、`metrics`、`trace`、`xerrors` | 日志、配置、可观测性和错误处理 |
| 基础设施 | `connector`、`db` | 外部连接与数据库访问 |
| 业务能力 | `cache`、`idgen`、`dlock`、`idem`、`mq` | 可复用的业务基础能力 |
| 服务治理 | `auth`、`ratelimit`、`breaker`、`registry` | 认证、流量治理和服务发现 |

每个组件都可以独立导入：

```go
import "github.com/ceyewan/genesis/cache"
```

具体配置和用法见各组件目录下的 `README.md`、GoDoc 与 `example_test.go`。

## 常用命令

```bash
make help
make check
make test-race
make examples
```

真实集成测试通过 `internal/testkit` 自动启动 Testcontainers，无需预先执行
`make up`。`make up` 只用于运行依赖外部服务的示例。

## 文档

- [文档入口](docs/README.md)
- [设计取舍](docs/design.md)
- [开发指南](docs/development.md)
- [发布流程](docs/release.md)
- [示例](examples/README.md)
- [内部测试工具](internal/testkit/README.md)

## License

MIT
