# xerrors

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/xerrors.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/xerrors)

`xerrors` 是 Genesis 的 Level 0 基础组件，提供一组与标准库 `errors` 完全兼容的轻量错误辅助工具。它的目标不是替代 `errors`，也不是构建一套复杂的错误框架，而是在不改变 Go 错误链语义的前提下，补齐几个高频且稳定的工程能力。

## 组件定位

`xerrors` 主要解决四类问题：

- 给错误追加上下文，同时保留 `errors.Is` / `errors.As`
- 给错误附加一个轻量的机器可读错误码
- 在初始化阶段提供 `Must` / `MustOK` 这类“失败即 panic”的辅助函数
- 在顺序校验流程里简化“保留第一个错误”与“合并多个错误”的写法

它**不**提供 stack trace、错误分类体系、并发安全的聚合器，也不负责统一 HTTP / gRPC / MQ 的协议层错误模型。

## 快速开始

```go
import "github.com/ceyewan/genesis/xerrors"
```

最常见的用法仍然是围绕标准库错误链做轻量封装：

```go
func loadConfig(path string) error {
    file, err := os.Open(path)
    if err != nil {
        return xerrors.Wrap(err, "open config file")
    }
    defer file.Close()
    return nil
}
```

## 核心能力

### 1. 错误包装

`Wrap` 和 `Wrapf` 用于给底层错误追加上下文，同时保留原始错误链：

```go
user, err := repo.FindByID(ctx, userID)
if err != nil {
    return nil, xerrors.Wrapf(err, "find user %d", userID)
}
```

### 2. 轻量错误码

`WithCode` 和 `GetCode` 适合做轻量的机器可读错误码传递：

```go
err := xerrors.WithCode(ErrUserNotFound, "USER_NOT_FOUND")

if code := xerrors.GetCode(err); code == "USER_NOT_FOUND" {
    // 映射到上层协议错误
}
```

这里的 `code` 只是一个字符串，不承担更复杂的错误元数据职责。

### 3. 初始化断言

`Must` 和 `MustOK` 只建议用于应用启动、依赖装配或测试辅助代码：

```go
cfg := xerrors.Must(config.Load("config.yaml"))
logger := xerrors.Must(clog.New(&cfg.Log))
```

运行时业务逻辑不应该依赖 `Must`，否则会把普通错误处理升级成进程级 panic。

### 4. 顺序错误收集与合并

`Collector` 的语义很窄：它只保留**第一个**非 `nil` 错误，适合顺序校验流程。它不是并发安全容器，也不是“收集所有错误”的聚合器。

`Combine` 则用于把多个错误合并成一个返回值：

- 全为 `nil` 时返回 `nil`
- 只有一个非 `nil` 错误时直接返回该错误
- 多个非 `nil` 错误时返回 `*MultiError`

## 推荐实践

- 业务代码里优先使用 `Wrap` / `Wrapf` 追加上下文，而不是重新丢失错误链。
- Sentinel error 仍然使用 `xerrors.New(...)` 定义，再通过 `errors.Is` / `xerrors.Is` 判断。
- 只有在确实需要协议映射或稳定机器码时，才使用 `WithCode`。
- `Collector` 适合“顺序校验多个字段，返回第一个错误”的场景；如果需要保留所有错误，应直接使用 `Combine` 或在上层定义更明确的数据结构。
- `Must` 仅用于初始化和测试，不应进入运行时业务分支。

## 能力边界

如果你的需求是：

- 自动采集 stack trace
- 统一建模公共错误结构
- 为 HTTP / gRPC / GraphQL 等协议自动生成错误响应
- 提供并发安全的错误聚合器

那么 `xerrors` 不是合适的组件。它的设计重点是**保持与标准库一致、接口极小、行为可预测**。

## 相关文档

- `go doc -all ./xerrors`
- [Genesis xerrors：轻量错误封装组件的设计与取舍](../docs/genesis-xerrors-blog.md)
- [示例代码](../examples/xerrors/main.go)
