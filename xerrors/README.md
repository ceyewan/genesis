# xerrors - Genesis 统一错误处理组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/xerrors.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/xerrors)

`xerrors` 是 Genesis 框架的统一错误处理组件，提供标准化的错误创建、包装和检查能力。

## 特性

- **零依赖设计**：不依赖任何 Genesis 组件，避免循环依赖
- **错误链兼容**：完全兼容 Go 1.13+ 的 `errors.Is`、`errors.As`、`errors.Unwrap`
- **Sentinel Errors**：提供 10 个预定义的通用错误类型
- **错误码支持**：机器可读的错误码，便于 API 错误映射
- **泛型支持**：Go 1.18+ 的泛型 `Must` 函数
- **智能错误聚合**：Collector 和 Combine 支持多错误处理

## 目录结构（完全扁平化设计）

```text
xerrors/                   # 公开 API + 实现（完全扁平化）
├── xerrors.go            # 错误包装、带码错误、多错误处理实现
├── xerrors_test.go       # 单元测试
└── README.md             # 本文档
```

**设计原则**：

- 完全扁平化设计，所有公开 API 和实现都在根目录
- 单文件实现，简单易用，无循环依赖问题
- 零依赖设计，仅使用 Go 标准库

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

### Sentinel Errors 检查

```go
result, err := cache.Get(ctx, key)
if xerrors.Is(err, cache.ErrCacheMiss) {
    // 缓存未命中，从数据库加载
    result, err = db.FindByID(ctx, id)
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

### Sentinel Errors

```go
var (
    ErrNotFound      = errors.New("not found")          // HTTP 404
    ErrAlreadyExists = errors.New("already exists")     // HTTP 409
    ErrInvalidInput  = errors.New("invalid input")      // HTTP 400
    ErrTimeout       = errors.New("timeout")            // HTTP 504
    ErrUnavailable   = errors.New("unavailable")        // HTTP 503
    ErrUnauthorized  = errors.New("unauthorized")       // HTTP 401
    ErrForbidden     = errors.New("forbidden")          // HTTP 403
    ErrConflict      = errors.New("conflict")           // HTTP 409
    ErrInternal      = errors.New("internal error")     // HTTP 500
    ErrCanceled      = errors.New("canceled")           // HTTP 499
)
```

### 错误包装函数

```go
// 用额外的上下文信息包装错误
func Wrap(err error, msg string) error

// 用格式化的上下文信息包装错误
func Wrapf(err error, format string, args ...any) error

// 添加机器可读的错误码
func WithCode(err error, code string) error

// 从错误链中提取错误码
func GetCode(err error) string
```

### Must 函数（仅用于初始化）

```go
// 如果 err 非空则 panic
func Must[T any](v T, err error) T

// 如果 ok 为 false 则 panic
func MustOK[T any](v T, ok bool) T
```

### 错误聚合

```go
// 错误收集器，保留第一个错误
type Collector struct { ... }
func (c *Collector) Collect(err error)
func (c *Collector) Err() error

// 智能合并多个错误
func Combine(errs ...error) error
```

### 标准库 Re-exports

```go
var (
    New    = errors.New
    Is     = errors.Is
    As     = errors.As
    Unwrap = errors.Unwrap
    Join   = errors.Join
)
```

## 使用示例

运行完整示例：

```bash
cd examples/xerrors
go run main.go
```

示例包含以下场景：

1. **基础错误包装** - Wrap/Wrapf 的多层链式调用
2. **Sentinel Errors 检查** - 各种预定义错误的判断
3. **带错误码的错误** - WithCode/GetCode 用于 API 错误映射
4. **错误收集** - Collector 用于表单验证等场景
5. **多错误合并** - Combine 用于聚合多个操作的错误
6. **初始化时 Must** - 展示 Must 的正确使用场景
7. **实战 API 场景** - 模拟真实的 HTTP API 错误处理流程

## 组件集成模式

### 定义组件特定错误

每个组件应该在自己的 `errors.go` 文件中定义 Sentinel Errors：

```go
// pkg/cache/errors.go
var (
    ErrCacheMiss     = xerrors.New("cache: miss")
    ErrKeyTooLong    = xerrors.New("cache: key too long")
    ErrSerialization = xerrors.New("cache: serialization failed")
)

// pkg/dlock/errors.go
var (
    ErrLockNotHeld   = xerrors.New("dlock: lock not held")
    ErrLockTimeout   = xerrors.New("dlock: acquire timeout")
    ErrAlreadyLocked = xerrors.New("dlock: already locked")
)
```

### 与 clog 配合使用

错误处理与日志记录应该分离：

```go
result, err := service.DoSomething(ctx)
if err != nil {
    // 记录日志（调用方的职责）
    logger.ErrorContext(ctx, "operation failed",
        clog.Error(err),
        clog.String("operation", "DoSomething"),
    )
    // 返回包装后的错误
    return xerrors.Wrap(err, "service.DoSomething")
}
```

## 最佳实践

| 场景       | 推荐做法                                                |
| ---------- | ------------------------------------------------------- |
| 业务逻辑   | `if err != nil { return xerrors.Wrap(err, "context") }` |
| 初始化     | `cfg := xerrors.Must(load())`                           |
| 多步骤验证 | 使用 `Collector` 收集第一个错误                         |
| API 错误   | 使用 `WithCode` 添加机器可读码                          |
| 日志记录   | 在调用方使用 `clog.Error`，不在 xerrors 内部            |
| 组件错误   | 定义 Sentinel Errors 在组件的 `errors.go` 文件          |

## 注意事项

1. **Must 仅用于初始化**：在运行时业务逻辑中使用 `Must` 会导致服务 panic
2. **不在 xerrors 中记录日志**：日志记录是调用方的职责
3. **保持错误链**：使用 `%w` 动词或 `Wrap` 函数，确保 `errors.Is/As` 可用
4. **组件定义自己的错误**：每个组件在 `errors.go` 中定义自己的 Sentinel Errors

## 架构设计

### 零依赖的意义

xerrors 是 Genesis 中唯一**零依赖**的包（除了 Go 标准库），这是刻意的架构决策：

1. **职责分离** - 错误创建与错误记录是独立的关切点
2. **避免循环依赖** - 防止与 clog 等组件形成循环依赖
3. **灵活性** - 错误处理与日志记录可以独立演进

### 在四层架构中的位置

```
┌────────────────────────────────────┐
│      Level 0 (Base)                │
├────────────────────────────────────┤
│                                    │
│  xerrors (零依赖)                  │
│    ↓ 被其他所有包使用               │
│                                    │
│  config ─→ 使用 xerrors            │
│  clog ─→ 使用 xerrors              │
│                                    │
└────────────────────────────────────┘
```

## 文档

- 使用 `go doc -all ./xerrors` 查看完整 API 文档
- 设计理念详见 `docs/foundation/xerrors-design.md`
- 更多示例见 `examples/xerrors/`

## License

[MIT License](../../LICENSE)
