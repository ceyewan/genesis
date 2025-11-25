# xerrors 设计文档

## 1. 概述

`xerrors` 是 Genesis 框架的统一错误处理组件，提供标准化的错误创建、包装和检查能力。

- **所属层级**：L0 (Base) — 最底层，无任何 Genesis 依赖
- **核心职责**：错误包装、Sentinel Errors、泛型 Must 函数

## 2. 设计原则

1. **零依赖**：不依赖任何 Genesis 包，避免循环依赖
2. **零副作用**：不做日志输出，由调用方决定如何处理错误
3. **兼容标准库**：完全兼容 `errors.Is`、`errors.As`、`errors.Unwrap`
4. **扁平化结构**：所有导出类型在 `pkg/xerrors/` 根目录

## 3. 目录结构

```text
pkg/xerrors/
├── xerrors.go       # 所有实现（接口、类型、函数）
└── xerrors_test.go  # 单元测试
```

## 4. API 设计

### 4.1 Sentinel Errors

预定义的通用错误类型，用于 `errors.Is` 判断：

```go
var (
    ErrNotFound      = errors.New("not found")
    ErrAlreadyExists = errors.New("already exists")
    ErrInvalidInput  = errors.New("invalid input")
    ErrTimeout       = errors.New("timeout")
    ErrUnavailable   = errors.New("unavailable")
    ErrUnauthorized  = errors.New("unauthorized")
    ErrForbidden     = errors.New("forbidden")
    ErrConflict      = errors.New("conflict")
    ErrInternal      = errors.New("internal error")
    ErrCanceled      = errors.New("canceled")
)
```

### 4.2 错误包装

```go
// Wrap 添加上下文信息，保留错误链
func Wrap(err error, msg string) error

// Wrapf 格式化版本
func Wrapf(err error, format string, args ...any) error

// WithCode 添加机器可读的错误码
func WithCode(err error, code string) error

// GetCode 从错误链中提取错误码
func GetCode(err error) string
```

### 4.3 Must 函数（仅用于初始化）

```go
// Must 如果 err 非空则 panic
func Must[T any](v T, err error) T

// MustOK 如果 ok 为 false 则 panic
func MustOK[T any](v T, ok bool) T
```

### 4.4 错误聚合

```go
// Collector 收集多个错误，只保留第一个
type Collector struct { ... }
func (c *Collector) Collect(err error)
func (c *Collector) Err() error

// Combine 合并多个错误为一个
func Combine(errs ...error) error
```

### 4.5 标准库 Re-exports

```go
var (
    New    = errors.New
    Is     = errors.Is
    As     = errors.As
    Unwrap = errors.Unwrap
    Join   = errors.Join
)
```

## 5. 使用示例

### 5.1 错误包装

```go
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, xerrors.Wrapf(err, "failed to read config file %s", path)
    }
    // ...
}
```

### 5.2 Sentinel 错误检查

```go
result, err := cache.Get(ctx, key)
if xerrors.Is(err, xerrors.ErrNotFound) {
    // 缓存未命中，从数据库加载
}
```

### 5.3 初始化时使用 Must

```go
func main() {
    cfg := xerrors.Must(config.Load("config.yaml"))
    db := xerrors.Must(database.Connect(cfg.DSN))
    // ...
}
```

### 5.4 多步骤错误收集

```go
func validateUser(u *User) error {
    var errs xerrors.Collector
    errs.Collect(validateName(u.Name))
    errs.Collect(validateEmail(u.Email))
    errs.Collect(validateAge(u.Age))
    return errs.Err()
}
```

### 5.5 带错误码的错误

```go
func GetUser(id int64) (*User, error) {
    user, err := db.FindUser(id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, xerrors.WithCode(xerrors.ErrNotFound, "USER_NOT_FOUND")
        }
        return nil, xerrors.WithCode(err, "DB_ERROR")
    }
    return user, nil
}

// 调用方
user, err := GetUser(123)
if err != nil {
    code := xerrors.GetCode(err) // "USER_NOT_FOUND" or "DB_ERROR"
    // 根据 code 返回不同的 HTTP 状态码
}
```

## 6. 最佳实践

| 场景 | 推荐做法 |
|-----|---------|
| 业务逻辑 | `if err != nil { return xerrors.Wrap(err, "context") }` |
| 初始化 | `cfg := xerrors.Must(load())` |
| 多步骤 | 使用 `Collector` 或 `Combine` |
| API 错误 | 使用 `WithCode` 添加机器可读码 |
| 日志记录 | 在调用方使用 `clog.Error`，不在 xerrors 内部 |

## 7. 注意事项

1. **Must 仅用于初始化**：在运行时业务逻辑中使用 `Must` 会导致服务 panic
2. **不在 xerrors 中记录日志**：日志记录是调用方的职责
3. **保持错误链**：使用 `%w` 动词或 `Wrap` 函数，确保 `errors.Is/As` 可用
