# Genesis 文档索引

> `Genesis` 是一套 Go 微服务基座组件集合。阅读顺序建议：先看 `usage_guide.md`，再根据所需组件深入对应文档。

## 入门指南
- `usage_guide.md`：快速启动、依赖注入示例、运行命令。
- `module_organization_guide.md`：了解仓库布局、如何放置实现与接口。
- `module_creation_guide.md`：新增组件时的 checklist 和质量要求。

## 核心组件
- `config.md`：配置管理接口与 Viper 实现要点。
- `clog.md`：结构化日志与上下文注入策略。
- `db.md`：数据库访问、事务封装与连接生命周期管理。
- `cache.md`：Redis 封装、缓存策略与分布式锁。
- `uid.md`：Snowflake ID 生成与 WorkerID 分配。
- `mq.md`：消息队列抽象，默认 Kafka 适配器。
- `coord.md`：etcd 协调器用法与接口约束。

## 服务治理与可观测性
- `ratelimit.md`：令牌桶限流器及本地/分布式实现策略。
- `breaker.md`：熔断、降级与自恢复流程。
- `once.md`：幂等保障与缓存依赖关系。
- `metrics.md`：指标上报规范与预置指标列表。
- `es.md`：搜索/分析组件的接口设计与演进计划。

## 开发辅助
- `usage_guide.md` 附录列出常用命令与调试技巧。
- 每个组件文档末尾提供测试与验收要点，实施前先核对。

> 若文档与代码出现偏差，以 `pkg/` 中定义的接口为准，并在每次迭代后更新对应文档。
