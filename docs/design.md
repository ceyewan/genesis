# 设计取舍

Genesis 保持单一 Go module 和扁平包结构：组件共用一套版本与依赖，避免应用承担
多模块版本组合问题。组件可以独立导入，但不能据此假设它们可以各自升级依赖。

应用显式装配并拥有生命周期。这个选择使启动代码较长，却能直接看出资源由谁
创建、共享和释放；组件库不需要再引入一个运行时 DI 或生命周期框架。

以下短文只记录选择的理由、代价与没有选择的方案，不复制接口、默认值或实现流程：

- [依赖与能力边界](./design/dependency-boundaries.md)。
- [状态与并发](./design/state-and-concurrency.md)。
- [服务调用治理](./design/service-governance.md)。
- [可观测性](./design/observability.md)。

当前行为查各包 GoDoc，使用示例查各包 README 和 examples。
