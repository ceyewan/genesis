# Genesis 总体设计

## 0. 摘要

Genesis 是一个 Go 微服务组件库，不是框架，也不试图替用户接管应用生命周期。

它当前的总体设计已经稳定在四个长期约束上：
- 依赖显式创建与注入，不使用运行时 DI 容器。
- 组件按治理、业务、基础设施和基础能力四层组织，但包结构保持扁平。
- 连接器拥有底层资源，业务组件借用资源并围绕能力建模。
- 文档语义必须和实现一致，不再用“看起来更强”的抽象掩盖后端差异。

这份文档的作用不是逐个解释所有组件，而是说明 Genesis 为什么这样分层、资源生命周期怎么划分、当前组件边界如何理解，以及测试和文档体系如何支撑这些约束。

## 1. 设计目标

Genesis 试图解决的不是“如何发明一个新框架”，而是“如何把一组高频基础能力沉淀成可组合的 Go 包”。这意味着它必须满足三点。

第一，初始化过程要足够透明。应用在 `main.go` 中应该一眼能看出配置、日志、连接器、组件和业务服务是怎样被组装起来的，而不是把依赖关系藏进容器或运行时反射里。

第二，组件语义要足够克制。统一入口可以有，但不能为了“抽象优雅”而把后端差异完全抹平。过去几轮审计里，`mq`、`idgen`、`dlock`、`registry`、`breaker`、`idem` 的主要问题几乎都不是代码不能跑，而是 API 承诺和真实实现之间存在裂缝。Genesis 现在的方向是宁可把边界写清楚，也不继续维持名不副实的强语义。

第三，资源所有权要清楚。连接池、长连接、会话、租约、watcher、watchdog 这些对象谁创建、谁关闭，必须可预测。Genesis 不使用中心化生命周期容器，因此资源释放只能依赖显式 `Close()` 和 Go 原生的 `defer`。

## 2. 分层模型

Genesis 保持四层模型，但强调“层次是能力分组，不是复杂目录层级”。

| 层次 | 核心组件 | 职责 |
| :--- | :--- | :--- |
| **Level 3: Governance** | `auth`, `ratelimit`, `breaker`, `registry` | 认证、流量治理、服务发现 |
| **Level 2: Business** | `cache`, `idgen`, `dlock`, `idem`, `mq` | 业务通用能力 |
| **Level 1: Infrastructure** | `connector`, `db` | 连接管理与数据库访问 |
| **Level 0: Base** | `clog`, `config`, `metrics`, `trace`, `xerrors` | 日志、配置、指标、追踪、错误模型 |

这四层的关键点不是“只能单向依赖”，而是“每层暴露的抽象类型不同”。

Level 0 暴露统一约束。日志、配置、指标、追踪和错误包装方式在这里定调，后续组件都要服从这些约束。

Level 1 暴露连接与底层访问能力。`connector` 负责 Redis、MySQL、Etcd、NATS、Kafka、SQLite 这类外部依赖的建立、连接、关闭和 client 暴露。`db` 在这里仍被视为基础设施层，因为它本质上是在 GORM 与分库分表能力之上提供统一数据库访问。

Level 2 暴露面向业务的通用能力。缓存、锁、ID 生成、幂等和消息队列都属于“业务会直接使用，但不关心底层驱动细节”的一层。

Level 3 暴露治理与切面能力。认证、限流、熔断和注册发现都会对请求路径或服务间调用行为产生影响，因此单独归到治理层。

## 3. 扁平结构与显式注入

Genesis 目录结构保持扁平，不再通过多层 `pkg/`、`internal/` 或子模块制造额外心智负担：

```text
genesis/
├── auth/
├── breaker/
├── cache/
├── clog/
├── config/
├── connector/
├── db/
├── dlock/
├── docs/
├── examples/
├── idgen/
├── idem/
├── metrics/
├── mq/
├── ratelimit/
├── registry/
├── testkit/
├── trace/
└── xerrors/
```

“扁平”不等于“没有结构”。Genesis 的结构主要体现在构造方式上，而不是体现在目录深度上。标准初始化流程是：

1. 加载配置。
2. 初始化 `clog`、`metrics`、`trace` 等基础能力。
3. 创建连接器。
4. 创建业务组件或治理组件，并显式注入 logger、meter、connector。
5. 创建业务服务和服务器。

这种方式的代价是 `main.go` 更长，但换来的是依赖关系完全可见。Genesis 明确接受这个取舍。

## 4. 资源所有权模型

Genesis 的资源模型只有一句话：谁创建，谁关闭。

这条规则落实到组件层面，形成两个明显角色。

连接器是资源所有者。它们创建连接池、socket、client、session，并负责在 `Close()` 中释放这些资源。连接器内部可以懒连接，也可以显式 `Connect()`，但资源生命周期必须可观察。

业务组件大多是资源借用者。它们基于 connector 暴露的 client 构建更高层能力，一般不拥有连接池本身。因此很多组件的 `Close()` 要么是 no-op，要么只负责关闭自己创建的 watcher、watchdog、session、subscription 这类附加资源，而不是反向关闭底层 connector。

过去这一点在多个组件里都出过问题，例如 `dlock.Close()` 曾经没有真正结束 Redis watchdog 和 Etcd 自建 session；`registry.Close()` 曾经吞掉租约撤销失败。当前设计已经明确要求：只要组件声明了 `Close()`，它就必须对自己创建的长期资源负责，并把关键失败显式返回给调用方。

## 5. 组件设计原则

Genesis 经过这一轮系统审计后，对组件设计形成了几条更具体的共识。

第一，统一入口不等于统一语义。`mq`、`dlock`、`ratelimit`、`registry` 这类多后端组件仍然会提供统一构造和统一主接口，但不会再假装所有驱动行为完全等价。驱动差异要么通过文档写清楚，要么通过显式错误暴露，要么在 API 上做能力收缩。

第二，文档必须低于或等于实现，不能高于实现。过去很多问题来自 README、go doc 或 blog 承诺过强，例如把 `idem` 说成 exactly-once，把 `registry.Watch` 说成避免事件丢失，把 `breaker` 的 fallback 说成能返回缓存对象。Genesis 现在的规则是：包文档定义边界，README 负责入口和推荐用法，blog 负责设计动机与取舍，但三者都必须服从真实行为。

第三，配置错误优先早失败。审计中多个组件都存在“非法配置被静默改成默认值”的问题，例如 `breaker`、`idem`、`dlock`。当前方向是将“缺省值填充”和“非法值校验”分开处理：用户没配可以补默认，用户显式传了危险值就应返回错误。

第四，错误模型尽量由组件自己定义，不直接把底层库错误裸露到边界上。例如 `breaker` 现在统一了打开状态和半开拒绝的错误语义，`dlock` 明确了 `ErrOwnershipLost`、`ErrInvalidTTL`，`idem` 明确了 `ErrLockLost`。这不是为了包一层而包一层，而是为了让调用方面对稳定的组件边界。

## 6. 当前核心组件边界

### 6.1 Level 0

`clog` 提供统一日志抽象和 context 透传，`config` 负责配置加载，`metrics` 和 `trace` 负责 OpenTelemetry 相关能力，`xerrors` 负责错误包装和 sentinel error。L0 组件的目标是统一约束，不是隐藏底层库的全部能力。

### 6.2 Level 1

`connector` 是 Genesis 里最重要的资源边界层。它负责建立连接、暴露底层 client，并保持一致的 `Connect`/`GetClient`/`Close` 生命周期模型。`db` 在此基础上提供数据库访问、事务与分库分表能力。

### 6.3 Level 2

`cache` 是双驱动缓存封装。`idgen` 目前提供 UUID v7、双模式 snowflake、Redis sequencer 与 Redis/Etcd allocator。`dlock` 提供 Redis/Etcd 分布式锁，并已经收紧 TTL 和 `Close()` 语义。`idem` 的定位已经从“exactly-once”收回到“结果复用型幂等组件”。`mq` 则明确成多后端消息组件，而不是强语义统一抽象。

### 6.4 Level 3

`auth` 当前是双 JWT 令牌模型，并保留 Gin 集成。`ratelimit` 采用单机与 Redis 分布式双模式，分布式路径基于 Redis 时间。`breaker` 是带场景错误分类的轻量熔断器。`registry` 则明确成单进程单 active registry 的 Etcd 注册发现组件，并把 gRPC resolver 的 endpoint 模型收紧成 gRPC-only。

## 7. 测试体系与 testkit

Genesis 的测试策略强调一条很实际的原则：集成测试必须贴近真实依赖，但不应该要求开发者先手动启动一整套本地环境。

因此 `testkit` 被定位成测试辅助包，而不是生产代码依赖。它当前提供三类能力：

- `NewKit`、`NewLogger`、`NewMeter`、`NewContext`、`NewID` 这类通用 helper。
- 基于 `testcontainers` 的容器化依赖启动能力，例如 Redis、MySQL、PostgreSQL、Etcd、NATS、Kafka。
- SQLite 这类无需容器的快速测试 helper。

测试约束也已经随之调整：

- 集成测试优先使用 `testkit.NewRedisContainerClient(t)`、`testkit.NewMySQLDB(t)` 这类 helper。
- 不要为了跑测试手动执行 `make up`；`make up` 只服务于 examples。
- 测试断言统一使用 `require`。
- 可复用的测试工具应当沉淀到 `testkit`，而不是散落在各组件测试里。

## 8. 文档体系

Genesis 的文档现在明确分成三层。

包注释和 `go doc` 负责定义组件定位、接口语义和边界。

每个组件目录下的 `README.md` 负责快速上手、适用场景和推荐用法。

`docs/` 下的 blog 文档负责解释为什么采用当前设计、有哪些工程取舍、哪些能力暂时不做。

这种分工来自一轮轮审计后的经验：只要三层文档混在一起，最终就一定会出现“README 说得比实现强，blog 说得比 README 强，go doc 最后没人信”的问题。

## 9. 当前阶段的结论

Genesis 现在已经不再追求“统一抽象越强越好”，而是强调“统一入口 + 清楚边界 + 资源语义稳定”。

这意味着未来继续演进时，有三条标准不会变：

- 新组件必须优先解释资源所有权和失败语义。
- 多后端组件必须正视驱动差异，而不是用同名 option 掩盖差异。
- 文档、测试和实现必须一起演进，不能只改其一。

如果把 Genesis 看成一个整体，它现在更像一套已经被系统校准过的 Go 微服务积木，而不是一个靠概念堆叠出来的大而全平台。
