# Genesis trace：全局链路追踪组件的设计与取舍

Genesis `trace` 是 Genesis 的 L0 基础组件，核心职责是提供统一的 tracing 初始化、上下文传播和消息链路辅助能力。它面向微服务和组件库场景，重点解决 OpenTelemetry `TracerProvider` 初始化、Gin / gRPC 自动传播，以及 MQ 异步消息场景下的链路关系建模问题。这篇文章不只介绍 `trace` 怎么用，更重点说明它为什么这样设计、适合什么场景，以及它和直接使用 OpenTelemetry Tracing SDK 之间的取舍。

## 0 摘要

- `trace` 不是完整的 tracing 平台客户端，而是 Genesis 内部统一 tracing 语义的一层薄抽象
- 它基于 OpenTelemetry，但当前更偏“初始化与传播 helper”，而不是完整 tracing 框架
- 当前实现采用全局模式：`Init()` 和 `Discard()` 都会安装全局 `TracerProvider` 与传播器
- Gin / gRPC 中间件保持薄包装，重点在于统一接入方式，而不是重造官方中间件
- MQ 场景下提供 producer / consumer helper，并支持 `link` 与 `child_of` 两种关系建模
- `Discard()` 虽然不导出 trace 数据，但仍然会修改全局 tracing 状态

---

## 1 背景与问题

链路追踪和日志、指标最大的不同在于：它天然依赖上下文传播。只要传播链条里某一段断掉，整条 trace 就会失真。HTTP、gRPC 这类标准协议还比较容易接入，因为官方中间件已经帮你做了大部分自动提取和注入；真正复杂的是系统里总会出现异步消息、定时任务、回调、数据库插件和跨服务自定义协议，这些地方如果没有统一约定，trace 很快就会变成“部分请求有、部分请求断”的状态。

直接使用 OpenTelemetry Tracing SDK 并不难，难的是让整个项目对初始化和传播形成统一理解。谁负责安装全局 `TracerProvider`？什么时候安装 propagator？MQ 消费端是把上游生产端当直接父 span，还是只做 link？这些问题如果让每个服务、每个组件自行决定，最后一定会得到多套互不兼容的 tracing 行为。

Genesis 需要自己的 `trace`，不是为了替代 OTel，而是为了把“怎么初始化 tracing、怎么传播 tracing、怎么建模异步消息关系”这些共识固化成一套统一约定。

---

## 2 基础原理与前置知识

理解 `trace` 之前，先要接受一个基础事实：**Tracing 天然偏全局状态**。只要应用中存在 HTTP 中间件、gRPC stats handler、数据库插件或任意依赖 `otel.GetTracerProvider()` 的库，它们就都会共享同一个进程级 `TracerProvider` 和 `TextMapPropagator`。这和普通业务对象的构造函数不一样，不是“new 一个实例只影响自己”。

第二个关键点是传播。`traceparent` 和 `baggage` 之类的信息本质上是通过 carrier 在进程边界之间传递的。HTTP Header、gRPC Metadata、MQ Header，本质上都是不同形式的 carrier。无论使用什么传输方式，真正的核心问题始终是：**上游如何注入，下游如何提取**。

第三个关键点是异步关系建模。同步 RPC 调用通常很自然地用 parent / child 关系串成单条 trace；但异步消息并不总适合这么建模。一个消息可能被延迟消费、批量处理、被多个消费者读取，甚至一个消费端处理对应多个上游生产事件。此时直接强行 parent / child 反而会扭曲链路。也正因为如此，`trace` 在 MQ 场景里同时保留了 `link` 和 `child_of` 两种模式。

---

## 3 设计目标

`trace` 的设计目标可以归纳为五条：

- **初始化统一**：把 tracing 初始化收敛到一个稳定入口，避免各服务自行拼装 exporter 和 provider
- **传播统一**：HTTP、gRPC、MQ 使用同一套传播语义和同一套全局 propagator
- **封装克制**：能直接复用官方中间件的地方，不重复造轮子
- **异步可表达**：对 MQ 场景提供标准 helper，并允许显式选择关系建模方式
- **副作用说清楚**：全局模式是现实约束，必须明确写入接口和文档，而不能假装它只是普通 helper

这几条目标决定了 `trace` 并不追求做一个“大而全的 tracing 平台接入层”。它当前更关注把最常见、最容易出错的初始化与传播路径做稳定。

---

## 4 核心接口与配置

`trace` 当前的公开接口主要分成三类。

第一类是初始化：

```go
func Init(cfg *Config) (func(context.Context) error, error)
func Discard(serviceName string) (func(context.Context) error, error)
```

第二类是传播：

```go
func Inject(ctx context.Context, carrier map[string]string)
func Extract(ctx context.Context, carrier map[string]string) context.Context
```

第三类是接入和 MQ helper：

```go
func GinMiddleware(serviceName string) gin.HandlerFunc
func GRPCServerStatsHandler() stats.Handler
func GRPCClientStatsHandler() stats.Handler
func StartProducerSpan(...)
func StartConsumerSpanFromHeaders(...)
func MarkSpanError(span oteltrace.Span, err error)
```

配置模型很薄：

| 字段 | 说明 |
| --- | --- |
| `ServiceName` | 服务名，必填 |
| `Endpoint` | OTLP gRPC 端点 |
| `Sampler` | 采样率，范围 0 到 1 |
| `Batcher` | `batch` 或 `simple` |
| `Insecure` | 是否使用非 TLS 连接 |

这里最需要讲清楚的是两条契约。

第一，`Init()` 当前采用**全局模式**。它不仅创建 `TracerProvider`，还会安装 OpenTelemetry 全局 `TracerProvider` 和全局 `TextMapPropagator`。因此它更像应用启动时的一次性初始化动作，而不是一个局部构造器。

第二，`Discard()` 也采用全局模式。它的真实语义不是“给我一个局部无副作用的 tracer”，而是“安装一个不导出数据、但仍能生成 TraceID 并维持传播的全局 provider”。这个区别必须说透，否则调用方很容易误判它的副作用范围。

---

## 5 核心概念与数据模型

`trace` 的心智模型可以概括成四个概念：**全局 provider**、**传播器**、**中间件包装** 和 **异步消息关系**。

### 5.1 全局 provider

和 `metrics` 类似，`trace` 当前不是“局部创建一个独立 tracer world”，而是“初始化一套进程级 tracing 状态”的模式。Gin 中间件、gRPC stats handler、数据库 tracing 插件以及调用 `otel.Tracer(...)` 的业务代码，都会共享这套全局状态。

### 5.2 传播器

`Inject` 和 `Extract` 的存在，看起来只是两个小 helper，但它们实际上把 MQ 和其他自定义 carrier 的传播路径统一了。调用方不需要每次都自己拼 `propagation.MapCarrier` 或自己记传播器组合是什么，只需要知道“把当前上下文注入 headers，或从 headers 恢复上下文”。

### 5.3 中间件包装

`trace` 并没有在 Gin 和 gRPC 上发明自己的追踪协议，而是直接复用 `otelgin` 和 `otelgrpc`。这种设计非常克制：Genesis 组件的职责不是重复实现官方中间件，而是给出统一、稳定的接入入口。

### 5.4 异步消息关系

`MessagingMeta` 和 `MessagingTraceRelation` 是 `trace` 在 MQ 场景里最重要的数据模型。

`link` 的语义是：消费者 span 和上游生产者 span 有关联，但不把它直接当父节点。它适合异步、延迟、批处理和多消费者场景，因为这些场景下消息处理并不天然等价于同步调用链的继续。

`child_of` 的语义是：消费者 span 直接把上游 span 当 parent，用单条 trace 串起生产和消费。它更适合演示、排障和少量强顺序异步流程，但不应滥用到所有消息场景。

---

## 6 关键实现思路

### 6.1 初始化链路

`trace.Init()` 的主链路可以概括为：

```text
校验 Config -> 创建 OTLP gRPC exporter -> 创建 resource -> 创建 TracerProvider -> 安装全局 provider 与 propagator -> 返回 shutdown
```

这里最重要的不是步骤本身，而是初始化结果的作用域。只要 `Init()` 成功，之后整个进程中通过 `otel.GetTracerProvider()` 或 `otel.Tracer(...)` 拿到的 tracer，都会共享这套 provider。

### 6.2 为什么中间件包装保持很薄

`GinMiddleware()` 和 `GRPC*StatsHandler()` 的实现都很薄，本质上只是把官方中间件用 Genesis 的命名重新暴露出来。这样做不是偷懒，而是一种取舍：HTTP / gRPC tracing 的复杂性已经在 OTel 官方中间件里得到验证，Genesis 更应该把注意力放在“统一接入入口”和“文档说清边界”，而不是再在这一层重新发明行为。

### 6.3 MQ 传播链路

`StartProducerSpan()` 做三件事：

- 用指定 tracer 启动 producer span
- 写入标准消息属性
- 把当前 span 上下文注入 headers

`StartConsumerSpanFromHeaders()` 则先从 headers 提取远端 span context，再根据 `TraceRelation` 决定关系建模方式：

- `child_of`：把提取到的远端 context 当作 parent
- `link`：不改变当前父子关系，只给 consumer span 加 link

这套设计最大的价值在于，它把“MQ 关系该怎么画”从调用方的隐式选择，变成了一个显式配置项。

### 6.4 错误标记为什么单独提供 helper

`MarkSpanError()` 本身很小，只是把 `RecordError` 和 `SetStatus(codes.Error, ...)` 收到一个 helper 里。但它的意义在于统一语义。否则不同业务代码会有人只 `RecordError`，有人只 `SetStatus`，有人两个都不做，最终 trace 平台上的错误呈现就会不一致。

---

## 7 工程取舍与设计权衡

### 7.1 为什么接受全局模式

理论上，最纯粹的设计是把 provider 创建和全局安装彻底拆开；但在当前 Genesis 的使用模式里，应用启动时本来就需要统一初始化 tracing，Gin / gRPC / DB / MQ 也都天然要共享同一套状态。因此当前版本接受全局模式，是一种现实且务实的选择。

但接受这种模式，不等于把它藏起来。真正的问题从来不是“用了全局状态”，而是“用了全局状态却不告诉调用方”。

### 7.2 为什么 `Discard()` 仍然存在

有些场景下，应用确实希望本地生成 TraceID、维持传播链路，却不把数据导出到后端。例如示例程序、本地开发或没有 tracing 基础设施的环境。`Discard()` 的作用就是提供这样一种“无导出 provider”。

它的问题不在功能本身，而在容易被误解成“局部 helper”。因此保守方案不是删除它，而是把它的全局副作用明确写透。

### 7.3 为什么 MQ 默认用 `link`

很多 tracing 初学者会天然倾向于用 `child_of`，因为它能把整条链路串成一条很直观的 trace。但真实的异步系统往往没有这么线性。消息重试、延迟处理、批量消费、多个消费者共享同一消息主题时，强行 parent / child 反而会让链路语义失真。Genesis 让 `link` 成为默认值，就是为了让默认行为更接近真实异步系统。

### 7.4 为什么当前 Config 还很薄

`trace.Config` 现在只覆盖最小的 OTLP gRPC 初始化参数，不包括 TLS、鉴权头、附加 resource attrs 等更复杂的配置。这不是因为这些能力不重要，而是因为当前组件更优先解决“统一初始化”和“统一传播”这两个主问题。对 L0 组件来说，先把边界收清楚，再逐步扩展，通常比一开始就做成重配置层更稳。

---

## 8 适用场景与实践建议

`trace` 适合以下场景：

- 你在写微服务，希望统一 tracing 初始化和传播方式
- 你需要 Gin / gRPC / MQ 共用同一套 tracing 状态
- 你希望对异步消息关系建模有明确约束，而不是每处自己决定
- 你接受 tracing 初始化本身是应用 bootstrap 行为，而不是局部 helper

它不适合以下场景：

- 你需要在同一进程里维护多套彼此隔离的 `TracerProvider`
- 你需要高度自定义 exporter、TLS、认证头和 resource attrs
- 你只是写一个很小的程序，不需要统一 tracing 接入

推荐实践有四条。

第一，应用启动时统一调用一次 `Init()`，把返回的 shutdown 纳入统一关闭流程。

第二，HTTP / gRPC 场景优先使用内置 Gin 和 gRPC helper，而不是自己重新拼官方中间件。

第三，MQ 场景默认使用 `link`；只有在确实需要把生产和消费串成单条 trace 时，再显式改用 `child_of`。

第四，明确把 `Discard()` 当成“安装无导出全局 provider”的手段，而不是“随手拿一个本地 tracer”的便捷函数。

常见误区也很集中：

- 把 `Init()` 当成没有副作用的普通构造器
- 认为 `Discard()` 不会影响全局 tracing 状态
- 在异步消息场景里默认滥用 `child_of`
- 中断 `context.Context` 传播，导致 trace 链路断裂

---

## 9 总结

`trace` 的价值不在于“帮你少写几行 OTel 初始化代码”，而在于它把 Genesis 对 tracing 初始化、传播和异步消息关系建模的工程共识固化成了一套更稳定的接口与行为。它让应用作者不必在每个服务里重新决定 provider 怎么装、传播器怎么设、MQ span 关系怎么画。

如果要用一句话总结 `trace` 的设计原则，那就是：**接受 tracing 天然偏全局状态的现实，把副作用说清楚，再用统一 helper 降低传播和建模出错的概率。**
