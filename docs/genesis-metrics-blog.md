# Genesis metrics：全局指标组件的设计与取舍

Genesis `metrics` 是 Genesis 的 L0 基础组件，核心职责是提供统一的指标创建能力、Prometheus 暴露端点，以及 HTTP / gRPC 服务端 RED 指标封装。它面向微服务和组件库场景，重点解决 OpenTelemetry 指标初始化、全局 `MeterProvider` 管理、指标标签约定和常见服务端埋点复用的问题。这篇文章不只介绍 `metrics` 怎么用，更重点说明它为什么这样设计、适合什么场景，以及它和直接使用 OpenTelemetry Metrics API 之间的取舍。

## 0 摘要

- `metrics` 不是监控平台客户端，而是 Genesis 内部统一指标语义的一层薄抽象
- 它基于 OpenTelemetry，但对外收敛成更小的 `Meter` 接口和一组服务端埋点 helper
- 当前实现采用全局模式：`New()` 在创建 `Meter` 的同时，也会安装全局 `MeterProvider`
- 除通用 Counter / Gauge / Histogram 外，组件内置了 Gin 和 gRPC 服务端 RED 指标封装
- 当配置了正数 `Port` 且 `Path` 非空时，组件会在同一进程内暴露 Prometheus HTTP 端点
- `Shutdown()` 负责关闭 HTTP 服务和底层 provider，调用方应按“谁创建，谁关闭”原则调用一次

---

## 1 背景与问题

指标体系看起来比日志和 trace 更简单，因为它的输出是聚合数值而不是上下文丰富的事件。但在微服务里，真正让指标变难维护的，往往不是采样或存储，而是埋点方式和标签约定不统一。一个服务把接口耗时记为 `http_request_duration_seconds`，另一个叫 `http_server_duration`；一个服务把状态写成 `status=200`，另一个写成 `outcome=success`；有的直接暴露原始 URL path，有的只暴露路由模板。久而久之，看板、告警和跨服务对比就都会变得脆弱。

直接使用 OpenTelemetry Metrics API 当然可以完成这些工作，但它暴露的是一套底层能力，而不是 Genesis 想要的工程约定。业务侧一旦直接面对 provider、instrument、reader、exporter 等概念，就很容易把同一件事写出很多版本。指标组件真正需要解决的问题，不是“如何创建 counter”，而是“如何让整个项目用同一种方式定义指标、暴露指标和给指标打标签”。

Genesis 需要自己的 `metrics`，就是为了把这些约定固定下来。对应用层而言，它应该更像一个统一的指标入口，而不是一个需要每个服务重新拼装的 OTel 初始化脚本。

---

## 2 设计目标

`metrics` 的设计目标可以归纳为五条：

- **接口收敛**：上层组件依赖 `Meter`、`Counter`、`Gauge`、`Histogram`，而不是直接依赖 OTel 的完整 API
- **统一约定**：常见标签、操作名和结果值在组件层统一，避免各服务各写一套
- **开箱可用**：应用初始化后即可暴露 `/metrics`，不要求每个服务重复搭建 Prometheus 端点
- **埋点复用**：HTTP / gRPC 服务端 RED 指标有统一 helper，不把重复样板散落到业务里
- **行为可解释**：全局 `MeterProvider`、HTTP 服务生命周期、`Shutdown` 语义都必须说清楚

这几条目标决定了 `metrics` 看起来并不追求高度灵活。它更关心“跨服务一致”和“工程可维护”，而不是把 OpenTelemetry Metrics SDK 的所有细节都暴露给调用方。

---

## 3 核心接口与配置

`metrics` 的公开接口很小：

```go
type Meter interface {
    Counter(name string, desc string, opts ...MetricOption) (Counter, error)
    Gauge(name string, desc string, opts ...MetricOption) (Gauge, error)
    Histogram(name string, desc string, opts ...MetricOption) (Histogram, error)
    Shutdown(ctx context.Context) error
}
```

这个接口的关键不在于“比 OTel 少多少方法”，而在于它把调用方真正关心的事情收敛成了三类 instrument 和一个生命周期方法。业务组件不需要知道 `MeterProvider` 怎么构造、Prometheus reader 怎么挂、runtime metrics 怎么开，只需要知道“我能创建什么指标，以及何时关闭资源”。

配置模型也很克制：

| 字段 | 说明 |
| --- | --- |
| `ServiceName` | 服务名，必填 |
| `Version` | 服务版本 |
| `Port` | Prometheus HTTP 端点端口；必须大于 0 才会启动 HTTP 服务 |
| `Path` | Prometheus 路径，通常为 `/metrics`；为空时不启动 HTTP 服务 |
| `EnableRuntime` | 是否开启 Go runtime 指标 |

这里最需要讲清楚的是两条契约。

第一，`New()` 当前采用**全局模式**。它不只是返回一个 `Meter`，还会安装 OpenTelemetry 全局 `MeterProvider`。这样做的好处是仓库里依赖全局 provider 的埋点库能立即共享同一套状态，代价是重复调用 `New()` 会覆盖之前安装的全局 provider。

第二，`Shutdown()` 负责关闭 `metrics` 持有的所有资源，包括 Prometheus HTTP 服务和底层 `MeterProvider`。它不是幂等承诺接口，调用方应当按“谁创建，谁关闭”的原则使用它。

---

## 4 核心概念与数据模型

`metrics` 的心智模型可以概括成四个概念：**全局 provider**、**instrument 工厂**、**标签约定** 和 **服务端 RED 指标集**。

### 4.1 全局 provider

`metrics` 当前不是“局部创建一个独立 meter”的模式，而是“初始化一套进程级指标状态”的模式。对调用方来说，这意味着 `metrics` 更像应用 bootstrap 的一部分，而不是一个随手创建的小工具。

### 4.2 instrument 工厂

`Meter` 本身只负责创建三种基础指标：

- `Counter`
- `Gauge`
- `Histogram`

这是一个刻意的收敛。对多数业务场景来说，这三类已经足够表达请求量、在途数量、耗时分布和业务事件累积值。Genesis 没有在这一层继续暴露更复杂的 instrument 组合，而是优先保证接口统一。

### 4.3 标签约定

组件内置了一组通用标签常量，例如：

- `service`
- `operation`
- `method`
- `route`
- `status_class`
- `outcome`
- `grpc_code`

这些常量的价值不只是“少打几个字”，而是降低跨服务看板、聚合查询和告警规则的维护成本。指标平台最怕的不是值多，而是同一个含义在不同服务里有不同字段名。

### 4.4 服务端 RED 指标集

除了通用 instrument，`metrics` 还提供了两组封装好的服务端指标：

- `HTTPServerMetrics`
- `GRPCServerMetrics`

它们把请求总量和请求耗时作为一组稳定的 RED 指标集暴露出来，并在内部统一标签结构。这样一来，业务代码不需要在每个服务里重复造一套“请求计数 + 耗时直方图”的轮子。

---

## 5 关键实现思路

### 5.1 基于 OTel，但不把 OTel 全部暴露给上层

`metrics` 的底层实现直接建立在 OpenTelemetry Metrics SDK 之上。选择 OTel 的原因很直接：它已经是当前可观测性生态的事实标准，支持稳定的指标抽象和导出链路。

但 `metrics` 没有把完整的 OTel provider / reader / exporter / instrument API 暴露给上层。Genesis 需要的是统一的工程契约，而不是让每个组件自己决定如何与 OTel 交互。

### 5.2 初始化链路

`metrics.New()` 的主链路可以概括为：

```text
校验 Config -> 创建 resource -> 创建 Prometheus exporter -> 创建 MeterProvider -> 安装全局 provider -> 可选启动 HTTP 服务 -> 可选启动 runtime metrics
```

这里最重要的实现约束是两个。

第一，组件会在初始化时显式监听 Prometheus 端口。也就是说，如果端口冲突、端口非正数或路径为空导致不会启动监听，或者监听失败，`New()` 会直接返回错误，而不是先返回成功、再在后台日志里异步失败。这让 `/metrics` 端点是否可用变成了一个明确的启动期契约。

第二，Prometheus 暴露端点不是额外的外部进程，而是内嵌在当前服务中的一个 HTTP server。这个设计很直接，也更符合大多数微服务把 `/metrics` 当作本地管理端点的习惯。

### 5.3 服务端 RED 指标封装

`HTTPServerMetrics` 和 `GRPCServerMetrics` 都不是简单的 instrument 暴露，而是预先固定了一套标签模型。

HTTP 侧会统一记录：

- `service`
- `operation=http.server`
- `method`
- `route`
- `status_class`
- `outcome`

gRPC 侧则记录：

- `service`
- `operation=grpc.server`
- `method`
- `route`
- `grpc_code`
- `outcome`

这意味着组件把“跨服务能否复用同一张看板”的问题提前在封装层解决了。

### 5.4 为什么 Gin 中间件要收敛未命中路由

Gin 中间件里一个看似不起眼但很重要的细节是：如果请求没有命中路由模板，就把 `route` 标签收敛成 `unknown`，而不是直接写入原始 URL path。这样做是为了避免指标标签高基数。指标系统最怕把用户输入、资源 ID 或随机路径片段直接打进标签，否则内存和时序数据基数都会迅速膨胀。

---

## 6 工程取舍与设计权衡

### 6.1 为什么当前采用全局模式

OpenTelemetry 的 provider 天然带有进程级色彩。Genesis 当前选择让 `metrics.New()` 直接安装全局 `MeterProvider`，不是因为这是唯一正确的方式，而是因为它最符合当前仓库的整体使用模式：应用启动时统一初始化可观测性，之后由组件和第三方埋点共享这套全局状态。

这种设计的代价也很明确：重复初始化会覆盖之前安装的全局 provider。因此它更像“应用 bootstrap 行为”，而不是一个可以随处重复调用的普通工厂函数。

### 6.2 为什么内置 Prometheus HTTP 端点

很多团队最终使用 Prometheus 时，都会在每个服务里再写一遍 `/metrics` 端点暴露逻辑。Genesis 不想让这类样板代码分散在各处，于是把它放进了基础组件里。

这会让 `metrics` 比“纯 instrument 工厂”多一层资源生命周期管理，但换来的好处是更稳定的服务启动路径和更统一的暴露方式。对于微服务来说，这个取舍是值得的。

### 6.3 为什么不把标签灵活性放到最高

理论上，完全开放标签字段最灵活；但工程上，完全灵活往往等于完全不一致。Genesis 在服务端指标封装里固定标签结构，实际上是在用一点自由度换一致性。对于单服务作者来说，这也许显得保守；但对跨服务看板、聚合查询和告警模板来说，这是明显更省心的选择。

### 6.4 为什么 `Gauge` 只是进程内便捷封装

当前 `Gauge` 的 `Inc/Dec` 是通过进程内 map 维护状态再写出记录实现的。它的目标不是做一个跨实例共享状态容器，而是给调用方一个更顺手的本地接口。这意味着高基数标签组合会让内部状态增长，也意味着它更适合表达进程内在途数、队列长度、资源占用等相对稳定的维度，而不是任意用户级标签。

---

## 7 适用场景与实践建议

`metrics` 适合以下场景：

- 你在写微服务，希望统一指标接口和 Prometheus 暴露方式
- 你需要跨服务复用 HTTP / gRPC 服务端 RED 看板
- 你希望应用启动时一次性初始化一套全局指标状态
- 你不想在每个服务里重复拼装 OTel Metrics 初始化代码

它不适合以下场景：

- 你需要在同一进程里维护多套彼此隔离的 `MeterProvider`
- 你需要极细粒度控制底层 OTel reader/exporter 组合
- 你只是写一个很小的程序，不需要统一指标体系

推荐实践有四条。

第一，应用启动时初始化一次 `metrics`，而不是在业务流程里重复调用 `New()`。

第二，优先使用 `DefaultHTTPServerMetricsConfig` 和 `DefaultGRPCServerMetricsConfig`，在必须兼容历史命名时再局部覆盖指标名。

第三，标签应尽量稳定、低基数。像 `route` 这类聚合维度是合适的，`user_id`、`request_id` 这类几乎总是不合适的。

第四，把 `Shutdown()` 纳入统一资源关闭流程，尤其是在示例程序、测试和 CLI 场景里，不要只创建不释放。

常见误区也很集中：

- 把 `metrics.New()` 当成“没有副作用的局部工厂函数”
- 把原始 URL path 直接写成标签
- 在同一进程里重复初始化多套全局 provider
- 忘记关闭 `/metrics` HTTP 服务

---

## 8 总结

`metrics` 的价值不在于“帮你少写几行 OTel 代码”，而在于它把 Genesis 对指标命名、标签结构、Prometheus 暴露和服务端埋点复用的工程共识固化成了一套更稳定的接口与行为。它让应用作者不必在每个服务里重新决定 provider 怎么装、端点怎么起、RED 指标怎么命名、标签该怎么收敛。

如果要用一句话总结 `metrics` 的设计原则，那就是：**基于 OpenTelemetry，接受全局模式带来的约束，用更统一的指标语义换更低的系统维护成本。**
