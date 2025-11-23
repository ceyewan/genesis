# clog 实现审查报告

**日期:** 2025-11-23
**审查对象:** `pkg/clog`, `internal/clog`, `examples/clog`
**参考文档:** `docs/clog-design.md`, `docs/genesis-design.md`, `docs/specs/component-spec.md`

## 1. 总体评价

`clog` 组件的实现整体上**高度符合**设计文档的要求。代码结构清晰，API 封装得当，成功隐藏了底层 `slog` 的实现细节。

* **API 设计:** 符合 `docs/clog-design.md` 中的定义，`Logger` 接口和 `Config/Option` 配置模式实现正确。
* **功能完整性:** 实现了设计中要求的基础日志、Context 字段提取、命名空间派生、结构化错误处理等核心功能。
* **示例覆盖:** `examples/clog/main.go` 提供了非常详尽的示例，覆盖了绝大多数使用场景。

## 2. 发现的问题与改进建议

### 2.1. [High] 源码行号 (Source Location) 获取机制脆弱

**问题描述:**
目前在 `internal/clog/logger.go` 中调用 `slog.NewRecord` 时，传递了无效的 PC (Program Counter) 值 `1`：

```go
// internal/clog/logger.go:158
record := slog.NewRecord(time.Now(), slogLevel, msg, 1) 
```

为了修复行号，`internal/clog/slog/handler.go` 在 `ReplaceAttr` 中使用了硬编码的调用栈深度来重新获取 caller：

```go
// internal/clog/slog/handler.go:94
_, file, line, ok := runtime.Caller(6) // 依赖具体实现层级，极度脆弱
```

**风险:**
这种做法非常**脆性 (Brittle)**。一旦 `slog` 内部实现发生微小变动（例如增加了一层函数调用），或者在不同的 Go 版本中，这个魔术数字 `6` 就会失效，导致日志显示的行号错误，甚至引发 Panic。这也违背了 `slog` 的设计初衷（即应该由 `NewRecord` 接收正确的 PC）。

**建议修复方案:**
在 `internal/clog/logger.go` 的 `log` 方法中，使用 `runtime.Callers` 获取正确的 PC，并传递给 `slog.NewRecord`。移除 Handler 中 `runtime.Caller(6)` 的补救逻辑。

```go
// 伪代码示例
var pcs [1]uintptr
runtime.Callers(3, pcs[:]) // 需要根据实际调用层级调整 skip 值
record := slog.NewRecord(time.Now(), slogLevel, msg, pcs[0])
```

### 2.2. [Medium] 控制台颜色输出 (Console Color) 未实现

**问题描述:**
`Config` 结构体中定义了 `EnableColor` 字段，且设计文档中提到了该功能。但在 `internal/clog/slog/handler.go` 中，该功能目前标记为 `TODO`，并未实际实现。

```go
// internal/clog/slog/handler.go:150
// TODO: 如果需要颜色支持，可能需要自定义 TextHandler 或使用第三方库。
```

**现状说明:**
在开发环境下，缺乏颜色支持会降低日志的可读性（例如无法快速区分 ERROR 和 INFO）。

**建议:**
目前在文档中明确标注该功能暂不支持。未来可考虑引入轻量级的颜色库或自定义 `slog.Handler` 来实现。

### 2.3. [Low] Context 字段提取性能

**问题描述:**
`extractContextFields` 使用了遍历 slice 和反射的方式来提取字段。虽然符合当前需求，但在极高并发场景下可能存在微小的性能开销。

**建议:**
当前阶段无需优化，保持代码可读性优先。

## 3. 示例 (Example) 覆盖度审查

`examples/clog/main.go` 的覆盖度**优秀**，包含了：

* [x] **基础配置:** 默认配置、JSON/Console 格式切换。
* [x] **日志级别:** 级别设置与动态调整 (`SetLevel`)。
* [x] **字段类型:** 涵盖了 `String`, `Int`, `Any` 等多种强类型字段。
* [x] **错误处理:** 演示了 `clog.Error` 和 `clog.ErrorWithCode` 的结构化输出。
* [x] **Context 集成:** 演示了如何配置和自动提取 `TraceID` 等 Context 字段。
* [x] **命名空间:** 清晰展示了 `WithNamespace` 的层级派生效果。
* [x] **组件标识:** 区分了代码层级的 Namespace 和技术组件 (`clog.Component`) 的用法。
* [x] **路径裁剪:** 演示了 `SourceRoot` 配置如何简化日志中的文件路径。

## 4. 结论

`clog` 模块已达到**可发布/可集成**的标准，唯有 Source Location 的实现方式需要尽快重构以消除隐患。
