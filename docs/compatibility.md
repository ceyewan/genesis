# 兼容性范围

CI 验证的是仓库内的工具链、Testcontainers 后端与示例，不保证所有厂商版本、
操作系统和生产拓扑都经过验证。Go 最低版本查 `go.mod`，固定 CI 工具版本查
`.github/workflows/ci.yml` 和 `Makefile`，后端镜像查 `internal/testkit`。
不在文档复制这些版本表，避免与实际检查漂移。

普通与 race 全量测试需要真实后端；Docker 不可用是失败，不是跳过。
镜像 tag 可变，测试日志也不是不可变镜像证明。客户端版本由 `go.mod` / `go.sum` 固定。

后端升级必须审阅投递、TTL、编码、事务及租约变化，不只是修改镜像版本。
应用负责自己的凭证、网络、持久化、备份和业务验收；下游项目不是本库 CI 的固定依赖。

源码兼容检查见[API 检查输入](./api/README.md)，升级动作见[迁移说明](./migration.md)。
