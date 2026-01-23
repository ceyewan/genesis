# xerrors - Genesis 统一错误处理组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/xerrors.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/xerrors)

`xerrors` 是 Genesis 框架的统一错误处理组件，提供标准化的错误创建、包装和检查能力。

## 特性

- **零依赖设计**：不依赖任何 Genesis 组件，避免循环依赖
- **错误链兼容**：完全兼容 Go 1.13+ 的 `errors.Is`、`errors.As`、`errors.Unwrap`
- **错误码支持**：机器可读的错误码，便于 API 错误映射
- **泛型支持**：Go 1.18+ 的泛型 `Must` 函数
- **智能错误聚合**：Collector 和 Combine 支持多错误处理

## 快速开始

```go
import "github.com/ceyewan/genesis/xerrors"
```

### 基础错误包装

```go
// 使用 Wrap 添加上下文
file, err := os.Open("config.yaml")
if err != nil {
    return nil, xerrors.Wrap(err, "open config file")
}

// 使用 Wrapf 格式化上下文
user, err := db.FindByID(ctx, userID)
if err != nil {
    return nil, xerrors.Wrapf(err, "find user %d", userID)
}
```

### 带错误码的错误

```go
// API 错误处理
user, err := getUserFromDB(123)
if err != nil {
    code := xerrors.GetCode(err)
    switch code {
    case "USER_NOT_FOUND":
        return HTTPError(404, code)
    case "DB_ERROR":
        return HTTPError(500, code)
    }
}
```

### 初始化时使用 Must

```go
func main() {
    // 仅在初始化阶段使用 Must
    cfg := xerrors.Must(config.Load("config.yaml"))
    logger := xerrors.Must(clog.New(&cfg.Log))
    conn := xerrors.Must(connector.NewRedis(&cfg.Redis))
    defer conn.Close()
}
```

## API 参考

```go
// 错误包装
func Wrap(err error, msg string) error
func Wrapf(err error, format string, args ...any) error

// 错误码
func WithCode(err error, code string) error
func GetCode(err error) string

// 初始化时使用
func Must[T any](v T, err error) T
func MustOK[T any](v T, ok bool) T

// 错误聚合
type Collector struct
func (c *Collector) Collect(err error)
func (c *Collector) Err() error
func Combine(errs ...error) error

// 标准库再导出
var New, Is, As, Unwrap, Join
```

## 组件错误定义

各组件应在自己的 `errors.go` 中定义错误：

```go
// pkg/cache/errors.go
var (
    ErrCacheMiss = xerrors.New("cache: miss")
    ErrKeyTooLong = xerrors.New("cache: key too long")
)
```

## 最佳实践

| 场景       | 推荐做法                                                |
| ---------- | ------------------------------------------------------- |
| 业务逻辑   | `if err != nil { return xerrors.Wrap(err, "context") }` |
| 初始化     | `cfg := xerrors.Must(load())`                           |
| 多步骤验证 | 使用 `Collector` 收集第一个错误                         |
| API 错误   | 使用 `WithCode` 添加机器可读码                          |

## 注意事项

1. **Must 仅用于初始化**：在运行时业务逻辑中使用会导致服务 panic
2. **组件定义自己的错误**：每个组件在 `errors.go` 中定义自己的 Sentinel Errors

## License

[MIT License](../../LICENSE)
