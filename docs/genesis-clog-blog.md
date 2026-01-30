# Genesis clog：基于 slog 的微服务日志库设计与实现

Genesis `clog` 是一个基于 Go 标准库 `log/slog` 构建的微服务日志组件，目标是提供高性能、结构化、可扩展的日志能力，并与微服务常见的命名空间分层与 `context.Context` 传播方式自然对齐。

---

## 0. 摘要

- `slog` 把“日志事件”建模为 `Record`，把“字段”建模为 `Attr(key, Value)`，把“输出”抽象成 `Handler`（负责过滤、绑定上下文、编码与写出）。
- 工程实践中常见的说法“通过实现一个 Handler 扩展能力”，本质是：**将日志处理策略下沉到 Handler 层**（字段注入/改写/脱敏/采样/多路输出/格式化），并通过包装器（wrapper）对多个策略进行组合。
- slog 的高性能来自：**字段是类型化 Value，延迟到编码阶段才转成字节**；同时大量常见类型不需要走反射/`fmt.Sprintf`，减少分配与 GC 压力。
- `With(...)` 的核心语义是“预绑定字段”：它不会立即写日志，而是将 attrs 绑定到派生 Logger/Handler 上；输出阶段由 Handler 将“预绑定 attrs + 本次调用 attrs + 自动注入 attrs”合并编码并写出。

---

## 1. 背景：微服务日志要解决的“真实问题”

在微服务场景中，日志通常不仅用于排障，也用于检索、聚合与治理：

- **可检索**：使用 `trace_id`、`request_id`、`user_id`、`namespace` 等字段快速定位问题。
- **可聚合**：按字段聚合统计（错误码分布、慢请求比例、下游错误突增等）。
- **可治理**：字段命名稳定、敏感信息可脱敏、日志量可控（采样/限流）。
- **可观测**：日志与 trace/metrics 对齐（至少包含 trace_id/span_id）。

结论是：日志必须结构化，且最好有明确的扩展边界——这正是 `slog` 的设计重心，也是 `clog` 要做的事。

---

## 2. slog 是什么：一次日志调用的数据流

以下示例使用 slog 原生 API（用于说明概念）：

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
logger = logger.With(slog.String("service", "user-service"))

logger.Info("create user",
	slog.Int("id", 1),
	slog.String("email", "a@b.com"),
)
```

其内部处理过程可以概括为四步：

1. **把键值对变成 `Attr`**（例如 `slog.Int("id", 1)`）。
2. **把一次日志事件装进 `Record`**（time/level/msg/attrs/source 等）。
3. **交给 `Handler.Enabled` 做快速过滤**（例如当前 level=info，就跳过 debug）。
4. **交给 `Handler.Handle` 编码并写出**（JSON/Text/自定义协议）。

其中关键概念是 `Attr`（字段）与 `Handler`（输出与扩展边界）。

---

## 3. slog 核心概念：Attr / Value / Record / Handler

### 3.1 Attr：字段不是“字符串拼接”，而是结构化数据

`Attr` 是一个非常朴素的结构：`key + Value`。

关键点在 Value：它不是 `interface{}` 的“黑箱”，而是带 **Kind（类型标签）** 的值容器（例如 string/int64/float64/bool/time/duration/group 等）。

这带来两个直接收益：

- **避免 fmt/反射**：常见类型可以走专门的编码路径，而不是 `fmt.Sprintf("%v")`。
- **延迟格式化**：先将结构化数据写入 Record，在输出阶段再编码为 JSON/Text 字节序列。

### 3.2 Record：把“构建日志事件”和“输出日志事件”解耦

`Record` 是“一次日志事件”的载体：时间、级别、消息、调用点（source）、attrs 列表……都在里面。

这层解耦的价值在于：Logger 层负责“描述事件”，Handler 层负责“处理事件”（如何输出、输出到何处、是否改写字段、是否采样等）。

### 3.3 Handler：slog 的扩展核心（也是边界）

`Handler` 负责三件事：

- `Enabled(ctx, level)`：**极快的过滤**（热路径）；不该打的日志要尽早返回。
- `Handle(ctx, record)`：把 record 编码并写出。
- `WithAttrs(attrs)` / `WithGroup(name)`：创建“绑定了额外上下文”的新 handler（预绑定字段、字段分组）。

可以将 `Handler` 理解为日志系统的“处理管线入口”：

- Logger/业务代码负责构建 `Record` 并提供结构化字段；
- Handler 决定过滤、注入、改写、编码与输出策略。

---

## 4. 扩展边界：通过实现 Handler 扩展日志能力

工程实践中常见的表达“实现一个 handler 即可扩展功能”，通常指的是：当需求不希望改变业务侧的记录方式（API 形状不变），而仅希望改变“日志事件如何被处理与输出”时，该需求应归属于 Handler 层。

典型需求及其落点包括：

- **字段注入**：自动加 `service/env/version`，从 `context.Context` 提取 `trace_id`、`user_id`。
- **字段改写/脱敏**：手机号/身份证打码，统一字段命名，删掉超大字段。
- **采样/限流**：对 debug/info 日志采样，错误日志全量保留。
- **多路输出**：同时写 stdout + 文件，或 stdout + 远端收集器。
- **格式化策略**：JSON 字段顺序、时间格式、level 表示、source 字段裁剪等。

### 4.1 组合方式：包装器（wrapper/middleware）

slog 并不提供“链式 handler API”，但 Handler 天然适合包装器（wrapper/middleware）模式：上层 handler 持有下游 handler，并将 `Enabled/Handle/WithAttrs/WithGroup` 委托给下游，同时在前后插入策略逻辑，从而达到可组合的扩展效果。

示意代码如下：

```go
type redactHandler struct{ next slog.Handler }

func (h redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	// 可在此处遍历/改写 r 的 attrs（或通过 ReplaceAttr 的思路集中改写）
	return h.next.Handle(ctx, r)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return redactHandler{next: h.next.WithAttrs(attrs)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{next: h.next.WithGroup(name)}
}
```

组合时可形成如下结构：

```
base(JSON/Text) -> wrapper(A) -> wrapper(B) -> wrapper(C)
```

该结构在效果上等价于“链式处理”，区别在于组合方式由结构（wrapper 嵌套）表达，而不是由 fluent API 表达。

### 4.2 Wrapper 的实现要点：正确转发 `WithAttrs/WithGroup`

`With(...)` 的预绑定字段语义最终需要通过 `WithAttrs`（以及 `WithGroup`）保持一致性：

- 若 wrapper 未正确转发 `WithAttrs`，则预绑定字段可能丢失或顺序异常；
- 若 wrapper 未正确处理 `WithGroup`，则字段分组结构可能发生偏差。

因此在实现 wrapper 时，不能仅实现 `Handle`，而应确保 `Enabled/Handle/WithAttrs/WithGroup` 形成一致且可组合的整体。

---

## 5. slog 语境下的“零拷贝/零分配”

这两个词在日志领域经常被滥用。更准确的说法是：

- slog 尽量让“字段构建”是**低分配**的：常见类型走类型化 Value，不需要 `fmt` 与反射。
- slog 把“把值变成字节”的成本延迟到编码阶段，且编码路径尽量少分配。

### 5.1 为什么 `Attr`/`Value` 有利于性能

对比两种写法：

- 字符串日志：常见实现依赖 `fmt.Sprintf` 或字符串拼接，容易在**调用点产生分配**；若最终被 level 过滤，则这部分开销无法回收。
- slog：字段以类型化 Value 存在，配合 `Enabled` 可以尽早返回，减少“无效日志”的构建成本。

### 5.2 “零拷贝”常见误解

只要最终要写到 `io.Writer`（stdout/file/socket），就必然会有“把内容写出”的过程；严格意义上的“零拷贝”在这里并不现实。

但 slog 能做到的是：**不提前把所有字段格式化成字符串**，而是把值以结构化形式保存，最后一次性编码写出；这减少了中间态对象与 GC 压力。

---

## 6. Genesis clog 的设计：在 slog 之上补齐微服务常用能力

`clog` 的定位是 Genesis L0 基础组件之一，提供一套稳定、可复用、易注入的结构化日志能力。

### 6.1 对外 API：薄封装，保持 slog 心智模型

`clog` 的字段类型 `Field` **直接复用** `slog.Attr`：

- 在代码里：`type Field = slog.Attr`
- 在使用上：仅需 import `clog`，使用 `clog.String(...)`、`clog.Int(...)` 等构造字段。

这带来两个好处：

- 不重复发明字段抽象（减少概念与适配成本）。
- 字段构造函数返回值是一个小结构体，调用点几乎“零成本”。

日志接口也尽量克制：五级别 + Context 版本 + `With(...)` + `WithNamespace(...)` + 动态 `SetLevel(...)`。

### 6.2 配置与默认值：开箱即用

`clog.Config` 支持：

- `Format=json|console`（生产推荐 json，开发推荐 console + color）
- `Output=stdout|stderr|file path`
- `AddSource/SourceRoot`（调用点定位与路径裁剪）

### 6.3 函数式选项：把“策略”留在构造阶段

`clog` 的常见选项：

- `WithNamespace(...)`：命名空间（最终写入 `namespace` 字段，形如 `service.api.v1`）。
- `WithContextField(key, fieldName)`：从 `context.Context` 提取业务自定义字段。
- `WithTraceContext()`：自动提取 OpenTelemetry `trace_id/span_id`。

这些均属于“日志策略”，集中在构造阶段配置更符合依赖注入与可维护性，业务代码仅需传递 `ctx`。

---

## 7. 实现细节：clog 如何用 Handler 落地“策略”

本节结合 `clog` 的实现对齐前述抽象，以明确扩展能力的落点与组合方式。

### 7.1 Handler 构造链：base handler + wrapper

在 `clog` 内部（`clog/handler.go`）构造顺序大致是：

1. `resolveWriter`：stdout/stderr/file/buffer
2. `slog.HandlerOptions`：
    - `Level` 使用 `*slog.LevelVar`（支持运行时动态调整）
    - `ReplaceAttr` 统一改写 level/time/source
3. base handler：
    - `slog.NewJSONHandler` 或 `slog.NewTextHandler`
4. 可选 wrapper：
    - `coloredTextHandler`：对 Text 输出做着色（典型的 handler wrapper）
5. 最外层 wrapper：
    - `clogHandler`：封装 `LevelVar`，提供 `SetLevel/Flush`

上述构造链体现了通过 Handler 组合策略的落地方式：将能力封装为 handler 或 wrapper，并在构造阶段按需要插入。

### 7.2 ReplaceAttr：统一字段表现（level/time/source）

`ReplaceAttr` 的典型用途是“让输出对人/对机器更友好”，例如：

- `level`：把 slog 的 level 统一转成 `"DEBUG"|"INFO"|...`
- `time`：统一时间格式（例如 RFC3339 带毫秒）
- `source`：把默认结构改写为 `caller="file:line"`，并做路径裁剪（`SourceRoot`）

`ReplaceAttr` 可视作“编码前的统一字段改写入口”。

### 7.3 Context 字段注入：在 log() 里做，而不是在业务里做

`clog` 的 `InfoContext/DebugContext/...` 最终都会走到 `log(ctx, ...)`：

- 先把预绑定字段 + 本次调用字段拼起来
- 再根据 options 追加：
    - OTel `trace_id/span_id`
    - `WithContextField` 配置的任意字段
    - `namespace`

业务侧只要把 `ctx` 传下去，日志就天然具备“跨服务关联”能力。

---

## 8. With(...) 预绑定字段：从派生 Logger 到最终 JSON 输出

以下以 `clog` 的行为说明预绑定字段的工作机制。

### 8.1 预绑定：With 只是创建派生 Logger，不会立刻输出

在 slog 原生用法中，通常写法如下：

```go
base := slog.New(...)
child := base.With(slog.Int("id", 1))
```

在 clog 里等价的是：

```go
base, _ := clog.New(clog.NewProdDefaultConfig("genesis"))
child := base.With(clog.Int("id", 1))
```

此时仅发生预绑定：将 attrs 绑定到派生 logger/handler 的上下文中；不会创建 record、不会编码 JSON，也不会产生输出。

### 8.2 记录构建：一次 Info(...) 会创建 Record 并收集所有 attrs

例如调用：

```go
child.InfoContext(ctx, "create user", clog.String("email", "a@b.com"))
```

`clog` 内部大体会做：

1. 合并 attrs：
    - child 的预绑定 attrs（来自 `With(...)`）
    - 本次调用 attrs（`email`）
    - context 注入 attrs（`trace_id` 等）
    - namespace attrs（如果配置了）
2. 构建 `slog.Record`（包含 time/level/msg/source）
3. `record.AddAttrs(...)` 把 attrs 写入 record
4. `handler.Enabled(ctx, level)` 过滤
5. `handler.Handle(ctx, record)` 编码并写出

### 8.3 真正写 JSON：编码发生在 Handler.Handle

当 format=json 时，最终写 JSON 的地方在 `slog.NewJSONHandler(...).Handle`：

- 它会遍历 record 里的 attrs；
- 对每个 attr 的 Value 按 Kind 走不同编码分支；
- 把 key/value 直接写入输出（通常是一个内部 buffer，再 flush 到 `io.Writer`）。

注意：这就是 slog 里“延迟格式化”的落点——调用 `With/Info` 时并不需要把所有字段转成字符串，真正转成字节序列是在 `Handle` 阶段完成的。

---

## 9. 实战落地：微服务推荐用法（clog）

### 9.1 生产环境：JSON + stdout + AddSource

```go
logger, _ := clog.New(&clog.Config{
	Level:     "info",
	Format:    "json",
	Output:    "stdout",
	AddSource: true,
	SourceRoot: "genesis",
}, clog.WithNamespace("user-service"))
```

### 9.2 请求链路：Context 传播 + 自动注入 trace/request/user

```go
logger, _ := clog.New(clog.NewProdDefaultConfig("genesis"),
	clog.WithNamespace("user-service", "api"),
	clog.WithTraceContext(), // OTel trace_id/span_id
	clog.WithContextField("request_id", "request_id"),
	clog.WithContextField("user_id", "user_id"),
)

logger.InfoContext(ctx, "get user",
	clog.String("path", "/v1/users/1"),
	clog.Int("id", 1),
)
```

### 9.3 错误字段：轻量、带码、带堆栈（按场景选）

```go
logger.Error("db failed",
	clog.ErrorWithCode(err, "DB_CONN_001"),
	clog.String("db", "users"),
)
```

一般建议：线上默认使用 `Error`/`ErrorWithCode`，仅在确需定位的场景使用 `ErrorWithStack`（避免产生日志量与敏感信息风险）。

---

## 10. 最佳实践与常见坑

- **字段稳定性比“多写点信息”更重要**：优先保证 key 的一致性（方便检索与聚合）。
- **避免高基数字段滥用**：例如把完整 SQL、完整 request body 当字段，往往会拖垮日志系统。
- **把策略放在 handler/构造阶段**：字段注入、脱敏、采样等尽量集中管理，不要散落在业务里。
- **把 Context 当成日志“载体”**：能从 ctx 自动拿的就自动拿（trace/request/user），业务就少传一堆 fields。

---

## 11. 扩展指南：新增通用能力的落点

为 `clog` 增加通用能力（例如脱敏、采样、多路输出）时，最自然的落点是 `clog/handler.go`：

- 以 wrapper 的方式实现一个 `slog.Handler`，包住 base handler；
- 在 `newHandler(...)` 的构造链里插入相应的 wrapper；
- 必要时新增一个 `Option` 来启用/配置该能力（保持“策略集中在构造阶段”）。

`clog` 中已有可参考的 wrapper：

- `coloredTextHandler`：对 Text 输出增加彩色（展示了如何正确实现 `WithAttrs/WithGroup` 并转发）
- `clogHandler`：对 base handler 增加动态 level 与 flush（展示了如何做“能力适配”）
