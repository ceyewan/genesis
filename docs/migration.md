# 迁移说明

本页只记录升级时需要采取的动作，不重复当前 API、默认值或生命周期手册。
发布范围以 [GitHub Releases](https://github.com/ceyewan/genesis/releases) 为准；
主线修复不会改变已发布标签。

## Unreleased hardening after RC3

- Registry 改用 `registry.New(cfg, registry.WithEtcdConnector(conn), opts...)`；
  旧的位置式 connector 参数不再支持。缺失或未连接的 connector 返回 `registry.ErrConnectorNil`，
  同时匹配 `connector.ErrClientNil`。
- `auth.Config`、`config.Config`、`mq.RetryConfig`、`ratelimit.Limit` 和 HTTP/gRPC Metrics 配置
  补齐 JSON/YAML/mapstructure snake_case 标签。持久化配置应按新字段名迁移，例如
  `config.Config` 的 `FileType` / `filetype` 改为 `file_type`，`EnvPrefix` / `envprefix` 改为 `env_prefix`。
- `metrics.Gauge` 只保留 `Set`。原 `Gauge.Inc/Dec` 改用 `Meter.UpDownCounter` 的 `Add(+1/-1)`；
  自定义 Meter 实现和测试桩也必须增加工厂方法。新建的 UpDownCounter 从零开始，不继承 Gauge 的旧状态。
  `metrics.Config.CardinalityLimit` 默认 2000（0 使用默认），超限维度会归入 OTel 溢出系列；
  需要保留更多维度的应用应显式提高上限，并迁移位置式 Config 字面量。
- DB 仅共享 Connector 的 SQL 池，不再共享 GORM 插件/回调。依赖 Connector 上自定义插件或回调的应用，
  必须改为在组件自己的 `DB(ctx)` 句柄上安装。SQLite 内存库仍复用原连接池。
- Etcd Locker 改为首次加锁时创建 Session，启动探活不能再依赖 `dlock.New`。
  加锁请求取消不再影响已经获得的锁的续租；必须使用 `Unlock` / `Close` 释放。

- Redis ACL 新增 `RedisConfig.Username`，位置式字面量必须改为带字段名的写法。
  TLS 通过 `connector.WithRedisTLSConfig` 显式启用；生产配置需可信 CA 和主机名校验。
- `idem.Execute` / `Consume` 的 nil 回调返回 `ErrInvalidConfig`；正数 `DefaultTTL`、
  `LockTTL` 和 `Consume` TTL 小于 1ms 会被拒绝，负 TTL 不再回退为默认值。
  Memory 存储关闭后的操作返回 `idem.ErrClosed`。依赖这些旧行为的调用方需先修正输入。
- MQ 的 `Drain(ctx)` 超时只结束当前等待；需要取消 Handler 时调用 `Close()`，
  不要把等待超时视为强制关闭完成。单个 Subscription 的 Drain 超时行为不变。
- `dlock`、`idem`、`ratelimit` 的组件日志统一使用 `namespace`，不再添加 `component`；
  依赖该字段的日志查询需更新。
- Sequencer 正 TTL 小于一毫秒返回 `ErrInvalidInput`。超过 `2^53` 的整数分配精度
  已修正，耗尽不改变旧值或 TTL；不应依赖旧的浮点失真结果。
- Redis 广播 `FromLatest` 不再在空读后重置最新位置；检查应用是否曾补偿过漏读。
- Multi 的同实例回填与修改现在协调执行，等待支持取消。跨实例仍不广播失效，
  不能据此取消应用原有的一致性措施。

## RC2 → RC3

- `metrics.New` 增加函数式选项参数。普通 `metrics.New(cfg)` 不变，但保存构造函数
  的精确函数类型、替代实现或测试桩需要加入 `...metrics.Option`。
- `trace.AttrMessagingDestination`、`AttrMessagingOperation` 和
  `AttrMessagingConsumerGroup` 改用以 `.name` 结尾的 OTel 属性键。
  依赖旧键的查询、看板和自定义埋点需迁移。
- JetStream 自动建流使用稳定的首段 topic 范围。已有 durable 会复用消费进度，
  不兼容的过滤或起始策略会失败而不是重建。重新打开消费者时保留原始起始选项；
  未指定 `WithMaxInflight` 时保留 broker 值。
  具体可修改项见[MQ 启动语义](../mq/README.md#durable-启动与配置变更)。

## 较早版本 → v1

以下汇总 v0.5 与早期 RC 的升级动作；RC1 已包含其中一些契约，按实际升级起点选择。
RC1 → RC2 的批准替换与删除以 API 检查输入为准。

### 源码调整

| 旧用法或依赖 | 迁移动作 |
| :--- | :--- |
| `idgen.UUID()` 单返回值 | 接收并处理 `(string, error)` |
| IDGen TTL 使用整数秒 | 改为带单位的 `time.Duration` |
| Allocator 使用默认 1024 个 WorkerID | 默认改为 32；单机房布局需要 1024 时显式配置 |
| `ParseGeneratorID` 四个返回值 | 增加错误处理，拒绝负 ID 或未知模式 |
| Sequencer 达上限后回绕 | 处理 `ErrSequenceExhausted`，安排新命名空间或调整上限 |
| `GinMiddleware()` 返回值做类型断言 | 返回值已是 `gin.HandlerFunc`，直接注册 |
| `trace.Discard`、`Batcher: "simple"` | 使用 `InstallLocalProvider`、`BatcherImmediate`；导出仍是异步的 |
| `mq.DefaultRetryConfig` 共享值 | 改为调用 `mq.DefaultRetryConfig()` 获取独立配置 |
| 给 `xerrors.New/Is/As/Unwrap/Join` 重新赋值 | 这些名称已是函数，测试需使用显式替代依赖 |
| `cache.WithMeter` | 删除无埋点的选项；`RawClient()` 已返回 `*redis.Client`，无需断言 |
| breaker fallback 只返回 error | 改为 `(any, error)`；gRPC 替代结果需与 reply 类型兼容 |
| 配置结构使用位置式字面量 | 改为带字段名写法；新增字段和 `mapstructure` 标签均可能改变精确声明类型 |

外部 mock、wrapper 或第三方接口实现还需补齐新增生命周期能力：Loader 与 Idempotency
的 Close、MQ 与 Subscription 的 Drain、Message 的延迟 Nak、Registry 的 Shutdown
与 LeaseFailures、Locker 的 Lost。完整声明查 GoDoc，不在这里维护另一套接口副本。

Allocator 的 `Stop` 现在返回释放错误，关闭流程必须处理它；保活通道的错误表示丢失
所有权，正常关闭表示取消或停止。不能忽略该信号继续生成依赖此 WorkerID 的 ID。

### 已删除的公共入口

- 公共 `testkit` 已移至 `internal/testkit`。外部项目维护自己的 fixture，不能把 import
  直接改为内部路径。
- 四组示例 Proto 包也已内部化：`examples/grpc-registry/proto`、`examples/idem/proto`、
  `examples/observability/proto`、`examples/proto/demo/v1`。需要时复制 `.proto` 到自己的
  module，设置自己的 `go_package` 并重新生成。
- trace 的 HTTP 包装器已删除，改用 `otelhttp.NewHandler` / `otelhttp.NewTransport`。
- `mq.Transport` 依赖未导出的选项类型，已改为包内实现细节；业务扩展使用 Middleware。
  未使用的 registry 哨兵错误和 ratelimit 指标名也已移除。

精确删除及批准替换见[API 检查输入](./api/README.md)，不要通过重新生成历史基线
掩盖升级时的破坏性变化。

### 上线前的行为回归

- MQ 初始位置从默认“只看新消息”改为保留历史；需要旧行为时显式使用 `FromLatest`。
  同名 durable/group 已按 topic 隔离，升级时检查消费身份与历史进度。
  JetStream 的 `WithBatchSize` 是实例本地预取，`WithMaxInflight` 是共享消费者背压。
- HTTP/gRPC 幂等按 endpoint、请求 fingerprint 区分结果；相同 key 不同内容会冲突。
  多租户需从已认证上下文配置 identity scope，不能信任任意租户 Header。
  结果超限时成功响应不缓存，后续请求可能重执行；原始 key、请求体和 Memory 容量
  超限也会显式失败。检查应用的重放防护和错误映射，不只验证普通成功响应。
- 负 duration 不再静默默认；Etcd TTL 受秒级精度约束。检查配置单位和边界值。
- JSON 与 MessagePack 不兼容；切换缓存编码前清理旧条目或更换 key prefix。
- Auth 默认只从 Authorization 头读取令牌，query/cookie 来源需显式启用。
- Config 的环境变量按目标 schema 合并；嵌套字段覆盖不再隐藏文件中的兄弟字段。
  检查自定义变量命名是否仍符合目标字段路径。
- 构造函数复制配置后填默认值；不再依赖传入结构被回填，也不能通过修改原配置
  重新配置活跃组件。需要热更新时构造并替换实例。
- MQ 使用 Drain，Registry 使用 Shutdown 进行有期限的优雅退出；返回的 gRPC 连接
  仍由应用关闭。已关闭的锁、限流器拒绝新操作；不可把旧实例继续当作可重用对象。
- 限流默认保持 fail-open，但显式未知策略改为 fail-closed；后端不可用与额度耗尽
  分别返回不同协议错误。验证硬保护路径和故障策略。

当前接入与安全措施见[生产接入](./production.md)，逐组件的错误和参数范围查 GoDoc。
