# Genesis 文档

本文档目录包含 Genesis 项目的设计文档和规范。

## 文档列表

- [架构设计](./genesis-design.md) - Genesis 的总体架构设计和核心理念
- [Auth 设计与 JWT 原理](./genesis-auth-blog.md) - Auth 组件设计、JWT 底层机制与认证扩展方案
- [Breaker 设计与实现](./genesis-breaker-blog.md) - 服务级熔断、状态迁移与 gRPC 客户端降级策略
- [Cache 设计与实现](./genesis-cache-blog.md) - Cache 双驱动架构、TTL/序列化策略与实践建议
- [CLog 设计与实践](./genesis-clog-blog.md) - 统一日志抽象、上下文透传与命名空间分层
- [Config 设计与实践](./genesis-config-blog.md) - 多源配置加载、解码策略与运行时治理
- [Connector 设计与实现](./genesis-connector-blog.md) - 连接器生命周期管理与多后端统一接入
- [DB 设计与实现](./genesis-db-blog.md) - DB 组件初始化流水线、事务模型与分库分表实践
- [DLock 设计与实现](./genesis-dlock-blog.md) - Redis/Etcd 双后端分布式锁、续期机制与所有权保护
- [IDGen 设计与实现](./genesis-idgen-blog.md) - 序列号与雪花 ID 生成机制、边界与选型建议
- [Idem 设计与实现](./genesis-idem-blog.md) - 幂等执行、消息去重与 Gin/gRPC 集成策略
- [MQ 设计与实践](./genesis-mq-blog.md) - NATS/Redis Stream/Kafka 对比、消费者组与死信策略
- [Observability 全栈实践](./genesis-observability-blog.md) - 基于 OpenTelemetry 的 LGTM 栈集成与 Trace/Log 联动
- [RateLimit 核心原理](./genesis-ratelimit-blog.md) - 令牌桶算法、单机/分布式实现与接入语义
- [Registry 核心原理](./genesis-registry-blog.md) - 服务注册发现、Watch 增量同步与 gRPC Resolver 机制

## 架构概览

Genesis 采用四层扁平化架构：

| 层次                        | 核心组件                                       | 职责                         |
| :-------------------------- | :--------------------------------------------- | :--------------------------- |
| **Level 3: Governance**     | `auth`, `ratelimit`, `breaker`, `registry`     | 流量治理，身份认证，切面能力 |
| **Level 2: Business**       | `cache`, `idgen`, `dlock`, `idem`, `mq`        | 业务能力封装                 |
| **Level 1: Infrastructure** | `connector`, `db`                              | 连接管理，底层 I/O           |
| **Level 0: Base**           | `clog`, `config`, `metrics`, `trace`, `xerrors`| 框架基石                     |

## 相关链接

- [项目主页](../README.md) - 返回项目主页
- [组件列表](../README.md#-组件列表) - 查看所有组件
- [使用示例](../examples/) - 查看代码示例
