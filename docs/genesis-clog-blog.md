# Genesis Clog：基于 Slog 的结构化日志组件设计与实现

Genesis `clog` 是基础层（L0）的日志组件，基于 Go 标准库 `log/slog` 构建，提供高性能、结构化、可扩展的日志能力。它采用接口抽象设计，支持命名空间分层、Context 字段传播、动态级别调整等微服务常用特性。

---

## 0 摘要

- `clog` 通过 `Logger` 接口抽象底层 slog 实现，提供五级别日志（Debug/Info/Warn/Error/Fatal）及 Context 版本
- 字段类型 `Field` 直接复用 `slog.Attr`，零内存分配，避免重复抽象与适配成本
- 支持层级命名空间 `WithNamespace(...)`，形成点分隔的 `namespace` 字段（如 `service.api.v1`）
- 支持从 `context.Context` 自动提取字段，包括 OpenTelemetry `trace_id/span_id` 和业务自定义字段
- 动态级别调整基于 `slog.LevelVar`，运行时生效无需重启
- Handler 架构采用 Wrapper 模式，支持字段注入、脱敏、采样、多路输出等扩展

---

## 1 背景：微服务日志的核心诉求

在微服务场景中，日志系统需要同时满足以下要求：

- **可检索**：通过 `trace_id`、`request_id`、`namespace` 等字段快速定位问题
- **可聚合**：按字段统计错误码分布、慢请求比例、下游错误突增等
- **可治理**：字段命名稳定、敏感信息可脱敏、日志量可通过采样控制
- **可观测**：日志与 trace/metrics 对齐，至少包含 `trace_id` 和 `span_id`
- **可扩展**：不修改业务代码的情况下，集中管理日志策略

传统字符串日志（`fmt.Printf` 风格）难以满足上述需求，结构化日志成为必然选择。

---

## 2 核心设计

### 2.1 接口抽象

`clog` 通过 `Logger` 接口隐藏底层实现：

```go
type Logger interface {
    Debug/Info/Warn/Error/Fatal(msg string, fields ...Field)
    DebugContext/InfoContext/WarnContext/ErrorContext/FatalContext(
        ctx context.Context, msg string, fields ...Field)
    With(fields ...Field) Logger
    WithNamespace(parts ...string) Logger
    SetLevel(level Level) error
    Flush()
}
```

这种抽象带来两个好处：

- 业务代码不依赖 slog，便于后续替换底层实现
- 接口保持克制，避免方法过多导致学习成本高

### 2.2 配置模型

`clog.Config` 支持以下配置：

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Level` | string | `info` | 日志级别 |
| `Format` | string | `console` | 输出格式：`json` 或 `console` |
| `Output` | string | `stdout` | 输出目标：`stdout`、`stderr` 或文件路径 |
| `EnableColor` | bool | `false` | 是否启用彩色输出（仅 console 格式） |
| `AddSource` | bool | `false` | 是否添加调用源信息 |
| `SourceRoot` | string | - | 用于裁剪文件路径，获取相对路径 |

提供两个便捷工厂函数：

- `NewDevDefaultConfig(sourceRoot)`：开发环境，console 格式，彩色，带源码信息
- `NewProdDefaultConfig(sourceRoot)`：生产环境，json 格式，无颜色，带源码信息

### 2.3 函数式选项

扩展能力通过函数式选项注入：

```go
logger, _ := clog.New(clog.NewProdDefaultConfig("genesis"),
    clog.WithNamespace("user-service", "api"),
    clog.WithTraceContext(),                    // 自动提取 OTel trace_id/span_id
    clog.WithContextField("request_id", "request_id"),
    clog.WithContextField("user_id", "user_id"),
)
```

策略集中在构造阶段配置，业务代码只需传递 `ctx`，符合依赖注入原则。

---

## 3 Slog 核心概念：Attr / Record / Handler

### 3.1 Attr：结构化字段

`Attr` 是 `key + Value` 的朴素结构，关键在于 Value 的类型化设计：

```go
type Attr struct {
    Key   string
    Value Value
}

type Value struct {
    // Kind 标识类型：String, Int64, Float64, Bool, Time, Duration, Group 等
    kind  Kind
    // 存储具体值，避免 interface{} 的"黑箱"问题
    any   any
}
```

这带来两个直接收益：

- **避免 fmt/反射**：常见类型走专门编码路径，而非 `fmt.Sprintf`
- **延迟格式化**：先存结构化数据，编码阶段再转字节序列

### 3.2 Record：日志事件载体

`Record` 包含一次日志事件的所有信息：

```go
- time: 时间戳
- level: 日志级别
- msg: 日志消息
- source: 调用位置（file:line）
- attrs: 结构化字段列表
```

这层解耦让 Logger 层负责"描述事件"，Handler 层负责"处理事件"。

### 3.3 Handler：扩展边界

`Handler` 负责三件事：

- `Enabled(ctx, level)`：快速过滤，热路径优化
- `Handle(ctx, record)`：编码并写出
- `WithAttrs(attrs)` / `WithGroup(name)`：创建绑定了额外上下文的新 handler

可将 Handler 理解为日志系统的"处理管线入口"：字段注入、脱敏、采样、格式化等策略都在此层实现。

---

## 4 字段传播：With / Context / Namespace

### 4.1 With：预绑定字段

```go
child := logger.With(clog.String("user_id", "123"))
child.Info("request processed")  // 自动包含 user_id
```

`With` 创建派生 Logger，字段被"预绑定"而非立即输出。实际输出发生在调用 `Info` 等 Level 方法时。

实现上需要深拷贝 `baseAttrs`，避免派生 Logger 之间共享底层数组：

```go
func (l *loggerImpl) With(fields ...Field) Logger {
    baseAttrs := append([]slog.Attr(nil), l.baseAttrs...)  // 必须复制
    baseAttrs = append(baseAttrs, fields...)
    return &loggerImpl{..., baseAttrs: baseAttrs}
}
```

### 4.2 Context：自动提取字段

通过 `*Context` 版本方法自动从 `ctx` 提取字段：

```go
logger.InfoContext(ctx, "get user",
    clog.String("path", "/v1/users/1"),
)
```

提取顺序：

1. OTel `trace_id` / `span_id`（若启用 `WithTraceContext()`）
2. 业务自定义字段（通过 `WithContextField()` 配置）
3. 命名空间字段（通过 `WithNamespace()` 配置）

### 4.3 Namespace：层级命名

```go
api := logger.WithNamespace("user-service", "api")
order := api.WithNamespace("order")
```

生成的 `namespace` 字段为 `user-service.api.order`，适合按服务/模块过滤日志。

---

## 5 Handler 架构：Wrapper 模式

### 5.1 构造链

`clog` 的 Handler 构造顺序：

```go
1. resolveWriter          → stdout/stderr/file
2. slog.HandlerOptions    → LevelVar, ReplaceAttr
3. base handler           → slog.NewJSONHandler / slog.NewTextHandler
4. (optional) coloredTextHandler → 彩色输出包装
5. clogHandler            → 动态级别 + Flush 能力
```

### 5.2 ReplaceAttr：统一字段表现

`ReplaceAttr` 在编码前改写字段：

- `level`：统一转成 `DEBUG/INFO/WARN/ERROR/FATAL`
- `time`：统一格式为 `2006-01-02T15:04:05.000Z07:00`
- `source`：改写为 `caller="file:line"`，并根据 `SourceRoot` 裁剪路径

### 5.3 Wrapper 实现要点

正确实现 Wrapper 需要转发所有方法：

```go
type wrapper struct{ next slog.Handler }

func (w wrapper) Enabled(ctx context.Context, level slog.Level) bool {
    return w.next.Enabled(ctx, level)
}

func (w wrapper) Handle(ctx context.Context, r slog.Record) error {
    // 在此插入策略逻辑
    return w.next.Handle(ctx, r)
}

func (w wrapper) WithAttrs(attrs []slog.Attr) slog.Handler {
    return wrapper{next: w.next.WithAttrs(attrs)}
}

func (w wrapper) WithGroup(name string) slog.Handler {
    return wrapper{next: w.next.WithGroup(name)}
}
```

若未正确转发 `WithAttrs/WithGroup`，预绑定字段可能丢失或顺序异常。

---

## 6 性能考虑

### 6.1 类型化 Value 避免反射

```go
clog.Int("count", 42)      // 直接存储 int64，无反射
clog.String("key", "val")  // 直接存储 string，无反射
clog.Any("obj", obj)       // 走反射路径，慎用
```

### 6.2 Enabled 提前过滤

```go
if !l.handler.Enabled(ctx, slogLevel) {
    return  // 提前返回，避免构建 Record
}
```

配合 `slog.LevelVar` 支持运行时动态调整级别，热路径低开销。

### 6.3 "零分配"的边界

- Field 构造是零分配的（小结构体）
- 但最终写出必然有分配（buffer、字节序列）
- 更准确的说法是"低分配"而非"零分配"

---

## 7 错误字段设计

### 7.1 三级错误字段

| 函数                              | 输出结构                                                     | 适用场景       |
| ------------------------------- | -------------------------------------------------------- | ---------- |
| `Error(err)`                    | `err_msg="message"`                                      | 一般错误，大多数场景 |
| `ErrorWithCode(err, code)`      | `error={msg="...", code="..."}`                          | 需要错误分类/统计  |
| `ErrorWithStack(err)`           | `error={msg="...", type="...", stack="..."}`             | 需要定位问题     |
| `ErrorWithCodeStack(err, code)` | `error={msg="...", type="...", code="...", stack="..."}` | 严重错误排查     |

### 7.2 堆栈捕获

使用 `runtime.Callers` 获取调用栈，`skip=3` 定位到业务代码：

```go
0. runtime.Callers
1. getStackTrace
2. ErrorWithStack
3. 业务代码  ← 从这里开始
```

---

## 8 彩色输出设计

### 8.1 Layout 设计

```go
15:48:17.340 | INFO | handler.go:42 > User created 	user_id=123 email=test@example.com
↑           ↑  ↑    ↑                ↑               ↑
时间        分隔 级别 调用位置         消息           业务属性
(灰色)     (灰色)(彩色)  (灰色)        (白色)        (青色key,灰色value)
```

### 8.2 颜色映射

| 级别 | 颜色 | 说明 |
|------|------|------|
| DEBUG | 紫色 | 显眼但不刺眼 |
| INFO | 绿色 | 正常状态 |
| WARN | 黄色 | 警告 |
| ERROR | 粗体红色 | 错误 |
| FATAL | 红底白字粗体 | 致命错误 |

---

## 9 实战落地

### 9.1 生产环境配置

```go
logger, _ := clog.New(&clog.Config{
    Level:     "info",
    Format:    "json",
    Output:    "stdout",
    AddSource: true,
    SourceRoot: "genesis",
}, clog.WithNamespace("user-service"), clog.WithTraceContext())
```

输出示例：

```json
{
  "time": "2025-01-15T10:30:45.123+08:00",
  "level": "INFO",
  "caller": "handler.go:42",
  "namespace": "user-service",
  "trace_id": "7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c",
  "span_id": "3d4e5f6a7b8c9d0e",
  "msg": "user created",
  "user_id": "123",
  "email": "test@example.com"
}
```

### 9.2 开发环境配置

```go
logger, _ := clog.New(clog.NewDevDefaultConfig("genesis"),
    clog.WithNamespace("user-service"),
)
```

输出示例：

```go
15:48:17.340 | INFO | handler.go:42 > user created 	email=test@example.com user_id=123
```

### 9.3 动态调整级别

```go
logger.SetLevel(clog.DebugLevel)  // 运行时生效，无需重启
```

---

## 10 最佳实践与常见误区

### 10.1 推荐做法

- 字段命名保持稳定，方便检索与聚合
- 使用 `*Context` 版本方法，自动提取 trace_id/span_id
- 错误日志优先使用 `Error` 或 `ErrorWithCode`
- 高基数字段（如完整 SQL）慎用或脱敏

### 10.2 常见误区

- **误区 1**：用 `Any` 存储所有类型
  - 正确做法：优先使用类型化字段（`String`、`Int` 等）
- **误区 2**：在消息里放结构化信息
  - 正确做法：消息放人类可读描述，结构化信息放字段
- **误区 3**：忽略 Context 传播
  - 正确做法：全程传递 `ctx`，让 trace 自动关联
- **误区 4**：生产环境用彩色输出
  - 正确做法：生产环境用 json 格式，便于日志系统解析

---

## 11 扩展指南

为 `clog` 增加通用能力时，落点在 `handler.go`：

1. 以 Wrapper 模式实现 `slog.Handler`
2. 在 `newHandler(...)` 构造链中插入
3. 必要时新增 `Option` 启用该能力

可参考的已有实现：

- `coloredTextHandler`：展示如何正确实现 `WithAttrs/WithGroup`
- `clogHandler`：展示如何做能力适配（`SetLevel/Flush`）
