# Genesis 文档

接口、配置和默认值查各包 GoDoc；快速上手查组件 README；完整代码查
[examples](../examples/README.md)。本目录只维护跨组件说明和源码无法解释的设计取舍。

## 使用与维护

- [组件选型](./component-selection.md)：不同需求怎样选择、组合组件。
- [生产接入](./production.md)：安全、超时、退出和部署责任。
- [问题排查](./troubleshooting.md)：根据现象定位常见故障。
- [迁移说明](./migration.md)：升级时需要修改的代码与行为。
- [开发指南](./development.md)：环境、测试和检查入口。
- [性能测试](./performance.md)：Fuzz、benchmark 命令与测量限制。
- [发布流程](./release.md)与[安全政策](./security.md)。
- [兼容性范围](./compatibility.md)：什么经过验证、什么需要应用自行验证。

## 设计与检查资料

- [设计取舍](./design.md)：架构、依赖、并发、治理和可观测性决策。
- [API 检查输入](./api/README.md)：供 CI 使用的基线、例外和生成清单，不是使用手册。

历史博客、版本计划和 RC 验收记录不在主线重复维护；需要时查对应 Git 标签或提交。
已发布版本与 Release Notes 以 [GitHub Releases](https://github.com/ceyewan/genesis/releases) 为准。
