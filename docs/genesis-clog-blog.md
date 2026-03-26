# Genesis clog：基于 Slog 的结构化日志组件设计与取舍

Genesis `clog` 是 Genesis 的 L0 基础组件，核心职责是提供统一、克制、可组合的结构化日志接口。它面向微服务和组件库场景，重点解决日志接口统一、命名空间分层、Context 字段透传、运行时调级和错误字段稳定输出等工程问题。这篇文章不只介绍 `clog` 怎么用，更重点说明它为什么这样设计、适合什么场景，以及它和直接使用 `slog` 之间的取舍。

## 0 摘要

- `clog` 不是日志平台，而是 Genesis 内部统一日志语义的一层薄抽象
- 它基于 `log/slog`，但不把 `slog.Logger` 暴露给上层组件
- 通过 `With`、`WithNamespace` 和 `*Context` 方法，把字段传播和日志上下文组织做成显式能力
- 通过 `SetLevel` 支持运行时调级，通过 `Close` 补齐文件输出的资源释放语义
- 错误字段统一输出为 `error={...}` 结构，减少检索、索引和统计时的 schema 分裂
- `Fatal` 只记录 FATAL 级别日志，不负责退出进程，控制流交由应用层决定

---

## 1 背景与问题

微服务里的日志很容易沦为一种“看起来很多，实际不好用”的数据。原因通常不是日志库不够强，而是日志接口、字段命名、资源管理和上下文传播没有形成统一约定。业务代码里一旦出现手写字符串、字段命名不一致、错误结构随意变化、组件各自决定是否退出进程，日志就很难同时满足排障、统计、审计和观测联动的要求。

Genesis 需要自己的 `clog`，不是因为标准库 `slog` 不够好，而是因为项目需要一层更稳定的工程语义。上层组件不应该关心具体 handler 怎么构造，也不应该在每个组件里重新决定 trace 字段、命名空间、错误格式和资源释放方式。`clog` 的价值就在这里：它不是替代 `slog`，而是把 Genesis 对日志的共识固化为一套一致的接口与行为。

换句话说，`clog` 解决的不是“如何打印日志”，而是“如何让整个组件库以同一种方式记录日志”。

---

## 2 设计目标

`clog` 的设计目标可以归纳为五条：

- **接口稳定**：上层组件依赖 `clog.Logger`，而不是直接依赖 `slog` 的具体类型
- **显式传播**：字段、命名空间和 Context 提取都通过显式 API 完成，不做隐式魔法
- **结构统一**：错误字段、调用位置、命名空间等核心信息保持稳定 schema
- **资源可控**：文件输出要有清晰的所有权和释放语义，不能只打开不关闭
- **行为克制**：日志组件只负责记录，不接管应用控制流，不把“退出进程”藏进日志 API

这五条目标决定了 `clog` 看起来并不“功能繁多”。它刻意保持小接口，不追求把日志平台的所有能力都塞进组件里，而是优先保证契约清晰、行为可预测。

---

## 3 核心接口与配置

`clog` 的公开接口很小，但已经覆盖了大部分工程场景：

```go
type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)
    FatalContext(ctx context.Context, msg string, fields ...Field)

    With(fields ...Field) Logger
    WithNamespace(parts ...string) Logger
    SetLevel(level Level) error
    Flush()
    Close() error
}
```

这个接口的关键不在于“方法多不多”，而在于职责边界很清楚：

- `With` 负责预绑定字段
- `WithNamespace` 负责层级命名空间
- `*Context` 方法负责从 `context.Context` 提取预配置字段
- `SetLevel` 负责运行时调级
- `Close` 负责释放文件输出时持有的底层资源

这里最值得强调的是两个契约。

第一，`Fatal` 不退出进程。它只记录 FATAL 级别日志，把是否退出、退出码是多少、是否先做清理等决策交给应用层。日志组件如果直接 `os.Exit(1)`，会把控制流副作用塞进“记录日志”这个动作里，既不利于测试，也不利于复用。

第二，`Close` 是必要的，而不是装饰性的。对 `stdout` 和 `stderr` 来说它是 no-op，但当 `Output` 指向文件路径时，logger 的确持有底层文件句柄。Genesis 的资源所有权原则是“谁创建，谁释放”，`clog` 不能例外。

配置模型同样保持克制：

| 字段 | 说明 |
| --- | --- |
| `Level` | 日志级别，支持 `debug/info/warn/error/fatal` |
| `Format` | 输出格式，支持 `json` 和 `console` |
| `Output` | 输出目标，支持 `stdout`、`stderr` 和文件路径 |
| `EnableColor` | console 格式下是否启用彩色输出 |
| `AddSource` | 是否输出调用位置 |
| `SourceRoot` | 是否裁剪调用文件路径，方便输出相对路径 |

在此基础上又提供了两个默认配置：

- `NewDevDefaultConfig`：偏开发体验，console、彩色、带源码位置
- `NewProdDefaultConfig`：偏生产落地，json、stdout、带源码位置

---

## 4 核心概念与数据模型

`clog` 的心智模型其实很简单，可以概括成四个概念：`Logger`、`Field`、`namespace`、`Context` 字段。

### 4.1 Field

`Field` 直接复用 `slog.Attr`：

```go
type Field = slog.Attr
```

这不是为了炫技，而是一个很务实的选择。Genesis 不需要再发明一套自己的字段结构，也不需要做多余的适配层。这样既降低了实现复杂度，也减少了字段构造时的包装成本。

### 4.2 命名空间

`namespace` 用来组织日志来源，而不是替代业务字段。它主要解决“这条日志来自哪个服务、哪个模块、哪个子模块”的问题。推荐把它看成日志里的分层路径，例如：

```text
user-service
user-service.api
user-service.api.order
```

这比手写 `component=...` 更稳定，也更适合在组件派生过程中逐层叠加。

### 4.3 Context 字段

`clog` 不会从 `context.Context` 中胡乱提取值。只有在构造 logger 时通过 `WithContextField` 或 `WithTraceContext` 明确声明过的字段，才会在 `InfoContext`、`ErrorContext` 这类方法里被提取。这样可以避免“Context 里有什么就打一堆什么”的隐式行为。

### 4.4 错误结构

`clog` 当前把错误字段统一成 `error={...}` 结构，而不是一会儿打平，一会儿分组。轻量错误可以是：

```json
{"error":{"msg":"invalid input"}}
```

需要错误码时可以扩展成：

```json
{"error":{"msg":"invalid input","code":"ERR_INVALID_INPUT"}}
```

此外还提供 `ErrorWithStack` 和 `ErrorWithCodeStack`，在需要调试信息时输出错误类型和调用栈，但整体结构仍然统一在 `error={...}` 下。

这个决定看起来只是字段样式统一，实际上影响很大。日志检索、索引映射、错误统计和告警规则都依赖字段 schema 的稳定性。

---

## 5 关键实现思路

### 5.1 基于 `slog`，但不暴露 `slog`

`clog` 的底层实现建立在 `log/slog` 之上。选择 `slog` 的原因很直接：它是标准库的一部分，具备结构化字段、`Handler` 抽象、`LevelVar`、`Record` 等成熟能力，足够支撑 Genesis 当前需要的日志能力。

但 `clog` 不直接把 `slog.Logger` 暴露给上层。这样做的目的不是为了“再包一层”，而是为了保持上层组件契约稳定。只要 `clog.Logger` 不变，底层具体用 `slog` 还是别的实现，上层组件都不用改。

### 5.2 日志主链路

`clog` 的记录链路可以概括为：

```text
New -> 构造 options -> 构造 handler -> 记录日志时组装 attrs -> 提取 Context 字段 -> 追加 namespace -> 写出
```

这里最核心的动作有三个：

- 把 `baseAttrs` 和当前调用字段合并
- 从 Context 中提取 trace 字段和业务字段
- 在最终输出前统一追加 `namespace`

这让 logger 的派生行为保持显式：字段和命名空间不是立即输出，而是在真正调用 `Info` / `Error` 时统一参与组装。

### 5.3 `With` 与 `WithNamespace` 的不可变派生

`With` 和 `WithNamespace` 都属于“派生 logger”操作。派生的关键要求不是“能追加字段”，而是“派生后不能污染兄弟 logger”。这也是结构化日志里最容易被忽略的一点：切片共享一旦处理不好，兄弟 logger 就会互相覆盖字段或命名空间。

`clog` 现在在这两条路径上都显式复制底层切片，再做追加。这并不复杂，但它体现了一个重要原则：logger 派生应当表现得像不可变对象，而不是一个可以被旁路修改的共享容器。

### 5.4 Handler 架构

`clog` 的 handler 构造顺序是：

```text
resolveWriter -> HandlerOptions -> JSON/TextHandler -> optional coloredTextHandler -> clogHandler
```

其中：

- `resolveWriter` 负责 stdout、stderr、buffer 或文件路径的 writer 解析
- `ReplaceAttr` 负责统一 `level/time/source` 的表现形式
- `coloredTextHandler` 只在 console 彩色输出时启用
- `clogHandler` 负责动态级别与资源关闭等额外能力适配

这里有一个设计细节值得单独说：文件输出的关闭逻辑并不藏在 `Flush` 里，而是由 `Close` 明确承担。因为 `Flush` 和 `Close` 不是一回事，混在一起只会模糊资源语义。

---

## 6 工程取舍与设计权衡

`clog` 的很多实现决策，如果只看代码会觉得“很普通”，但它们恰好体现了组件设计最重要的取舍。

### 6.1 为什么 `Fatal` 只记录，不退出

早期很多日志库都把 `Fatal` 设计成“记录后退出”。这种设计看起来方便，但代价是日志 API 开始接管业务控制流。测试里调用一次 `Fatal`，整个进程就没了；示例代码里调用一次 `Fatal`，清理逻辑可能来不及执行；组件内部一旦误用 `Fatal`，上层应用甚至不知道哪里触发了退出。

Genesis 选择把控制权还给应用层。日志组件只表达“这是 FATAL 级别事件”，至于要不要退出、怎么退出、退出前是否关闭其他资源，由调用方决定。

### 6.2 为什么要补 `Close`

很多日志封装喜欢暴露 `Output: "/path/to/file.log"`，却不提供显式关闭能力。这在长生命周期服务里可能暂时不出问题，但只要你频繁创建 logger、在测试里创建文件输出、或在示例程序里重复运行，就会把文件句柄生命周期问题留给调用方猜。

Genesis 不想靠“默认用 stdout，所以大多数人感觉不到问题”来掩盖资源语义。既然支持文件输出，就应该把关闭语义暴露出来。

### 6.3 为什么统一错误字段结构

如果 `Error(err)` 打平为 `err_msg`，`ErrorWithCode(err, code)` 又输出为 `error={...}`，日志消费端就不得不兼容两套 schema。对于 Loki 查询、字段索引、错误聚合和告警规则来说，这种不一致很快就会变成长期负担。

统一 `error={...}` 的代价很小，却能显著提升日志结构稳定性。这是典型的“为了消费端和长期维护做设计，而不是只为了调用端少写几个字”。

### 6.4 为什么不宣称“零分配”

`Field` 直接复用 `slog.Attr`，这的确减少了字段适配成本；`Enabled` 先做级别检查，也能避免一些无意义的后续工作。但从整个写出链路看，日志编码、buffer 使用、console 彩色包装都仍然存在分配和字符串处理。

因此更准确的说法是：`clog` 在字段模型上尽量低成本，而不是“整条日志链路零分配”。这种表述听起来没那么夸张，但它更诚实，也更利于技术文档长期维护。

---

## 7 适用场景与实践建议

`clog` 适合以下场景：

- 你在写微服务或组件库，希望全项目共享一套日志接口
- 你需要把 trace 字段、请求字段和模块命名空间稳定地组织起来
- 你希望日志行为足够简单，不想在每个组件里重新组装 `slog.Handler`

它不适合以下场景：

- 你需要完整日志平台能力，例如日志轮转、多路写出、复杂采样规则
- 你追求极限性能，并愿意为了性能直接绑定更底层或更激进的日志库
- 你只是在一个很小的程序里打印几行日志，并不需要统一工程约束

推荐实践有四条。

第一，生产环境优先用 JSON 输出到 stdout，并打开源码位置：

```go
logger, _ := clog.New(
    clog.NewProdDefaultConfig("genesis"),
    clog.WithNamespace("user-service"),
    clog.WithTraceContext(),
)
defer logger.Close()
```

第二，开发环境优先用 `NewDevDefaultConfig`，保留彩色输出和调用位置。

第三，组件内不要手写公共字段，优先用 `WithNamespace` 和 `With` 派生子 logger。

第四，错误日志优先使用统一的错误字段函数，不要在消息字符串里拼接错误详情，更不要同时混用多套错误字段 schema。

常见误区也很集中：

- 把 `Fatal` 当成退出进程的快捷方式
- 在消息字符串里塞结构化信息
- 滥用 `Any` 打大对象
- 在生产环境里依赖彩色 console 输出
- 输出到文件却忘记 `Close`

---

## 8 总结

`clog` 的价值不在于“比 `slog` 多了多少功能”，而在于它把 Genesis 对日志的工程共识固化成了一套更稳定的接口和行为。它让组件作者不必重复思考字段结构、命名空间、Context 提取、错误输出和资源释放这些基础问题，从而把注意力放回业务能力本身。

如果要用一句话总结 `clog` 的设计原则，那就是：**基于标准库，保持接口克制，把日志从“打印行为”提升为“稳定的工程契约”。**
