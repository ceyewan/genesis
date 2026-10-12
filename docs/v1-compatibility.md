# Genesis v1 兼容矩阵

本矩阵描述当前仓库的验证配置，不保证所有厂商版本和部署拓扑都经过验证。
具体发布状态以 GitHub Releases 为准，发布过程见[发布流程](./release.md)。
历史 RC2 指定下游门禁与证据归档要求不适用于当前流程。

## 工具链

| 工具 | 版本 | 用途 |
| :--- | :--- | :--- |
| 最低 Go | `1.26.0` | 构建、选定包测试 |
| CI Go | `1.26.5` | 普通/race、lint、示例及 API 检查 |
| golangci-lint | `2.12.2` | 静态分析与导出注释 |
| Buf | `1.66.1` | 示例 Proto |
| actionlint | `1.7.7` | 工作流语法 |
| Node.js | `22.23.2` | CI Markdown 检查 |
| markdownlint-cli2 | `0.18.1` | Markdown 格式 |

本地其他补丁版本的结果可帮助开发，但不能替代对应 CI 工具链验证。
实际固定值以 `.github/workflows/ci.yml` 与 `Makefile` 为准。

## 公共包

所有根 module 公开包及 `cache/serializer` 都遵守同一 SemVer 契约。
`breaker`、`dlock`、`idem` 与其他组件一样具有兼容性责任；不以某个业务项目是否
导入它们决定稳定性。API 与生命周期见[契约决策](./v1-api-decisions.md)。

`internal/testkit` 和示例的 `internal/proto` 仅用于本仓库。外部应用应维护自己的
测试 fixture 与 Proto，不能导入这些内部路径。

## 外部后端

| 后端 | Testcontainers 镜像 | 验证组件 |
| :--- | :--- | :--- |
| Redis | `redis:7.2-alpine` | connector、cache、dlock、idem、idgen、mq、ratelimit |
| PostgreSQL | `postgres:17-alpine` | connector、db |
| MySQL | `mysql:8.0` | connector、db |
| Etcd | `quay.io/coreos/etcd:v3.5.12` | connector、dlock、idgen、registry |
| NATS JetStream | `nats:2.10-alpine` | connector、mq |
| Kafka | `confluentinc/confluent-local:7.5.0` | connector |
| SQLite | go.mod 中的进程内驱动 | connector、db |

普通与 race 全量测试运行真实后端；Docker 不可用是失败，不是跳过。
镜像 tag 可变，不能把这张表描述成不可变镜像证明；本 CI 保存测试日志，不维护
镜像摘要与发布证据哈希协议。客户端精确版本由 `go.mod` / `go.sum` 固定。

## 解释与升级

- 各后端的确认、精度和恢复语义保留差异，不承诺统一的 exactly-once 或强一致。
- 改变投递、TTL、事务或所有权语义的后端升级需要契约审阅，不只是修改镜像 tag。
- 暴露 Gin、gRPC、GORM、OTel、JWT 或原生客户端类型的 API 需要审阅上游兼容性。
- 依赖 PR 运行本库门禁；下游项目自行进行业务验收，不成为本库 CI 的固定依赖。
- 拓扑、凭证、TLS、持久化、备份和厂商升级由应用负责。

安全扫描与依赖策略见[安全与依赖治理](./v1-security-and-dependencies.md)。
