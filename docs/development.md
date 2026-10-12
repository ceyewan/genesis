# Genesis 开发指南

## 环境要求

- Go 1.26 或更高版本；CI 验证 1.26.0 与固定补丁版本 1.26.5。
- golangci-lint 2.12.2。
- Docker，用于 Testcontainers 集成测试和可运行示例。
- Node.js，用于 Markdown 检查。

仓库使用单一 `go.mod`。本地联合开发可以使用未提交的 `go.work`，CI 设置
`GOWORK=off`，不依赖其他项目源码。

## 常用命令

```bash
make check
make test-fast
make test-race
make examples-scenarios
make examples-external-check
make markdown-check
make api-compat-check
make examples
make example-all
```

`make check` 运行普通测试、lint、现代化检查、API 清单与兼容性检查、文档检查和
示例编译。涉及并发或生命周期时，还需运行 `make test-race`。
`test-fast` 是显式选定的无 Docker 包与用例，方便本地反馈，不代表全部单测通过，
也不替代任何普通/race 容器门禁。Fuzz 与性能命令见[性能基线](./performance.md)。

`make example-all` 只运行可自行结束的独立示例。场景示例通过
`make example-<name>` 单独运行；可观测性完整验收见
[示例说明](../examples/observability/README.md)。
`examples-check` 仅编译；`examples-scenarios` 真实运行四个治理/数据访问场景测试；
`examples-external-check` 在临时外部 module 编译 MQ/DLock 示例和生产装配文档。
后两者与全量测试都会执行行为验收，真实后端缺失会失败。

## 测试与资源

仓库测试通过 `internal/testkit` 自动管理容器；`make up` 只用于示例。
谁创建资源，谁负责关闭。Connector 拥有连接，上层组件只借用连接。

修改前阅读目标包 README、GoDoc 和测试；修改后补边界测试并运行对应检查。
使用 `gofumpt`、`goimports`、`xerrors` 和 Genesis 基础组件，保持显式装配。

## CI 边界

日常 CI 检查最低 Go 版本、全量普通和竞态测试、lint、Markdown 与链接、
工作流语法、Proto、导出注释、API 兼容性和独立示例运行。
测试和示例结果保存 14 天，用于排查问题，不维护发布哈希取证链。

Genesis CI 不检出或验证 Resonance 等下游仓库。下游项目负责自己的集成测试；
发布者按[发布流程](./release.md)完成本库验收。CI 不自动发布或部署。

## 文档约定

组件使用说明集中在各包 README 和 GoDoc；跨组件说明集中在本目录。
旧博客和发布记录仅作历史参考，不作为当前行为或发布状态的依据。
API 基线、兼容性例外和迁移说明仍属于活跃契约资料，不能随历史文档一起删除。
不为一次 AI 工作过程新增长期文档。
