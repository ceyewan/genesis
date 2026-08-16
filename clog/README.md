# clog

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/clog.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/clog)

`clog` 是 Genesis 的 L0 日志组件，基于 Go 标准库 `log/slog` 构建。它的目标不是替代日志平台，而是为 Genesis 各组件提供统一、克制、可组合的结构化日志接口，解决命名空间分层、Context 字段提取、运行时调级和统一错误字段输出等工程问题。

## 组件定位

- 提供稳定的 Logger 接口，避免上层组件耦合 `slog.Logger` / `slog.Handler`；`Field` 则刻意复用 `slog.Attr`
- 支持 `With` 和 `WithNamespace` 派生，方便按服务、模块、子模块组织日志
- 支持从 `context.Context` 自动提取业务字段与 OpenTelemetry Trace 字段
- 支持 JSON / console 两种输出格式，以及运行时动态调整级别
- 当输出到文件时，显式暴露 `Close()`，并遵循 Genesis 的资源所有权原则

`clog` 不负责日志采集、检索、告警、轮转和异步批处理。这些能力属于日志平台或应用层。

## 快速开始

```go
cfg := clog.NewProdDefaultConfig(projectRoot)
cfg.ServiceName = "user-service"
cfg.Version = "v1.0.0"

logger, err := clog.New(
    cfg,
    clog.WithNamespace("user-service", "api"),
    clog.WithTraceContext(),
    clog.WithContextField("request_id", "request_id"),
)
if err != nil {
    return err
}
defer logger.Close()

logger.Info("Request started",
    clog.String("path", "/v1/users"),
    clog.String("method", "GET"),
)
```

## 核心能力

| 能力 | 说明 |
| --- | --- |
| 结构化字段 | `Field` 直接复用 `slog.Attr`，减少字段适配成本 |
| 命名空间 | `WithNamespace("service", "api")` 生成 `namespace=service.api` |
| Context 提取 | 通过 `WithContextField` 和 `WithTraceContext` 自动注入上下文字段 |
| 动态级别 | `SetLevel()` 基于 `slog.LevelVar`，运行时生效；非法级别返回错误 |
| 错误结构 | 统一输出 `error={...}`，便于检索、索引和统计 |
| 文件输出 | 只有 `New()` 创建的根 logger 持有底层文件句柄，调用方需要关闭根 logger |

`Config.ServiceName`、`Version`、`InstanceID`、`Environment` 会稳定输出为 `service.name`、`service.version`、`service.instance.id`、`deployment.environment.name`。生产 JSON 日志建议与 trace/metrics 使用同一组值。

## 默认配置

`New(nil)` 与 `New(&clog.Config{})` 行为一致，都会使用零值默认配置：

- `Level=info`
- `Format=console`
- `Output=stdout`

如果需要显式的开发或生产默认值，请使用 `NewDevDefaultConfig(sourceRoot)` 或 `NewProdDefaultConfig(sourceRoot)`；参数用于裁剪调用源码路径，不是服务名。

## 推荐使用方式

### 生产环境

- 使用 `json` 格式，输出到 `stdout`
- 打开 `AddSource`，便于排障
- 配合 `WithTraceContext()` 关联 trace
- 组件内使用 `WithNamespace()` 派生，不要手写 `namespace` 字段

### 开发环境

- 使用 `NewDevDefaultConfig(...)`
- 使用 `console` 格式和颜色输出
- 保留源码位置，方便快速定位

### 错误日志

- 大多数场景使用 `Error(err)`
- 需要错误分类时使用 `ErrorWithCode(err, code)`
- 只有在定位复杂问题时再使用带堆栈的错误字段
- `Fatal` 只记录 FATAL 级别日志，不会退出进程；进程生命周期由应用层控制

## 资源释放

当 `Output` 为文件路径时，`New()` 返回的根 logger 会持有底层文件句柄：

```go
logger, _ := clog.New(&clog.Config{
    Level:  "info",
    Format: "json",
    Output: "/var/log/app.log",
})
defer logger.Close()
```

`With()` / `WithNamespace()` 派生出的子 logger 不拥有底层资源，`Close()` 是 no-op。
对 `stdout`、`stderr` 和 `Discard()` 返回的 logger，`Close()` 也是 no-op。

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/clog)
- [组件设计博客](../docs/genesis-clog-blog.md)
- [Genesis 文档目录](../docs/README.md)
