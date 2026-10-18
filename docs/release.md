# Genesis 发布流程

发布由维护者手动执行，CI 不自动创建标签、Release 或部署服务。
Genesis 不要求 Resonance 或其他指定下游仓库通过验收才能发布。

## 发布前

1. 通过 PR 合入主线，确认目标提交的全部 CI 检查通过。
2. 审阅 [API 兼容检查](./api/README.md)与[升级策略](./migration.md)；
   首版发布前按最终候选生成稳定基线，发布后冻结。v1 不以例外放行破坏性改动。
   运行 `make vulncheck` 并按[安全政策](./security.md)审阅结果，存在未处置的生产安全问题时不要发布。
3. 如改动涉及可观测性或场景示例，运行相应真实环境验收。
4. 确定新版本号与 Release Notes，列出修复、接口变化和已知限制。
5. 记录发布提交及 CI 链接即可，不维护额外的证据哈希、标签注解协议或下游清单。

## 发布

确认版本与提交后，由维护者创建新的标签并通过 `gh release create` 发布。
推送 `v*` 标签会运行本库 CI；等待其全部通过后再公开 Release。
可先创建 draft；尚在验证阶段的版本标记为 prerelease。
已发布标签不移动、不覆盖；后续修复使用新版本。

## 发布后

在隔离临时目录使用 `GOWORK=off go mod download` 检查公开模块可下载，
更新 README 中的版本信息。下游项目自行升级、运行集成测试并反馈问题。

发布版本、提交和 Release Notes 以
[GitHub Releases](https://github.com/ceyewan/genesis/releases) 为准，
本页不重复维护发布状态或历史验收记录。
