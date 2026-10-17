# Genesis 开发指南

## 环境要求

- Go 最低版本以 `go.mod` 为准，CI 验证版本以工作流为准。
- golangci-lint 等质量工具的固定版本见 `Makefile` 与工作流。
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
也不替代任何普通/race 容器门禁。Fuzz 与性能命令见[性能测试](./performance.md)。

`make example-all` 只运行可自行结束的独立示例。场景示例通过
`make example-<name>` 单独运行；可观测性完整验收见
[示例说明](../examples/observability/README.md)。
`examples-check` 仅编译；`examples-scenarios` 真实运行四个治理/数据访问场景测试；
`examples-external-check` 在临时外部 module 编译文档引用的 MQ、DLock、Connector
和缓存查询示例，验证公开 API 接入不依赖仓库 internal 包。
场景验收和全量测试使用真实后端，后端缺失会失败；外部复制检查只编译，不连接服务。

## 测试与资源

仓库测试通过 `internal/testkit` 自动管理容器；`make up` 只用于示例。
谁创建资源，谁负责关闭。Connector 拥有连接，上层组件只借用连接。

修改前阅读目标包 README、GoDoc 和测试；修改后补边界测试并运行对应检查。
使用 `gofumpt`、`goimports`、`xerrors` 和 Genesis 基础组件，保持显式装配。

## CI 边界

日常 CI 检查最低 Go 版本、全量普通和竞态测试、lint、Markdown 与链接、
工作流语法、Proto、导出注释、API 兼容性和独立示例运行。
测试和示例日志用于排查问题；保留策略见工作流，不维护发布哈希取证链。

Genesis CI 不检出或验证 Resonance 等下游仓库。下游项目负责自己的集成测试；
发布者按[发布流程](./release.md)完成本库验收。CI 不自动发布或部署。

## 文档约定

- API、配置、默认值和错误语义放在 GoDoc，不在 docs 再抄完整声明与字段表。
- 组件 README 提供最短接入路径，完整可运行代码放 examples；文档引用代码而非复制。
- docs 只保留跨组件操作指南、迁移动作、政策和源码无法解释的设计取舍。
- 不强制一组件一篇博客，不维护重复总结、旧版本计划或一次工作过程的验收日志。
- 历史资料查 Git 标签、提交和 Releases，不为删除文档另建一套归档目录。
- [API 检查输入](./api/README.md)供 CI 使用；稳定基线发布后冻结，不维护预发行兼容例外。

迁移路径时同步修复 Markdown、Go 注释和 Makefile 引用，并运行 `make markdown-check`。
修改 API 资料还需运行 `make api-inventory-check api-compat-check`，不能只确认链接可达。
