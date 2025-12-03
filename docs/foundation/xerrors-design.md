# xerrors 设计文档

## 1. 概述

`xerrors` 是 Genesis 框架的统一错误处理组件，提供标准化的错误创建、包装和检查能力。

* **所属层级**：L0 (Base) — 最底层，无任何 Genesis 依赖
* **核心职责**：错误包装、Sentinel Errors、泛型 Must 函数
* **设计原则**：
  * 零依赖：不依赖任何 Genesis 包，避免循环依赖
  * 零副作用：不做日志输出，由调用方决定如何处理错误
  * 兼容标准库：完全兼容 `errors.Is`、`errors.As`、`errors.Unwrap`
  * 扁平化结构：所有导出类型在 `pkg/xerrors/` 根目录

## 2. 目录结构

```text
pkg/xerrors/
├── xerrors.go       # 所有实现（接口、类型、函数）
└── xerrors_test.go  # 单元测试
```

## 3. API 设计

### 3.1 Sentinel Errors

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

### 3.2 错误包装

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

### 3.3 Must 函数（仅用于初始化）

```go
// Must 如果 err 非空则 panic
func Must[T any](v T, err error) T

// MustOK 如果 ok 为 false 则 panic
func MustOK[T any](v T, ok bool) T
```

### 3.4 错误聚合

```go
// Collector 收集多个错误，只保留第一个
type Collector struct { ... }
func (c *Collector) Collect(err error)
func (c *Collector) Err() error

// Combine 合并多个错误为一个
func Combine(errs ...error) error
```

### 3.5 标准库 Re-exports

```go
var (
    New    = errors.New
    Is     = errors.Is
    As     = errors.As
    Unwrap = errors.Unwrap
    Join   = errors.Join
)
```

## 4. 与其他组件的集成

### 4.1 Connector 错误

Connector 使用 xerrors 定义错误：

```go
// pkg/connector/errors.go
var (
    ErrNotConnected  = xerrors.New("connector: not connected")
    ErrAlreadyClosed = xerrors.New("connector: already closed")
)

// 使用时包装
func (c *redisConnector) Connect(ctx context.Context) error {
    if err := c.client.Ping(ctx).Err(); err != nil {
        return xerrors.Wrapf(err, "redis connect to %s", c.cfg.Addr)
    }
    return nil
}
```

### 4.2 组件错误

每个组件定义自己的 Sentinel Errors：

```go
// pkg/dlock/errors.go
var (
    ErrLockNotHeld   = xerrors.New("dlock: lock not held")
    ErrLockTimeout   = xerrors.New("dlock: acquire timeout")
    ErrAlreadyLocked = xerrors.New("dlock: already locked")
)

// pkg/cache/errors.go
var (
    ErrCacheMiss = xerrors.New("cache: miss")
    ErrKeyTooLong = xerrors.New("cache: key too long")
)

// pkg/ratelimit/errors.go
var (
    ErrRateLimited = xerrors.New("ratelimit: rate limited")
)
```

### 4.3 Config 错误

```go
// pkg/config/errors.go
var (
    ErrConfigNotFound = xerrors.New("config: file not found")
    ErrInvalidConfig  = xerrors.New("config: invalid format")
    ErrValidation     = xerrors.New("config: validation failed")
)
```

### 4.4 错误处理模式

```go
// 业务代码中的错误处理
result, err := cache.Get(ctx, key)
if xerrors.Is(err, cache.ErrCacheMiss) {
    // 缓存未命中，从数据库加载
    result, err = db.FindByID(ctx, id)
    if err != nil {
        return xerrors.Wrap(err, "load from db")
    }
}

// 使用 WithCode 添加业务错误码
if xerrors.Is(err, xerrors.ErrNotFound) {
    return xerrors.WithCode(err, "USER_NOT_FOUND")
}
```

### 4.5 与 clog 配合

错误应在调用方记录日志，而非在 xerrors 内部：

```go
result, err := service.DoSomething(ctx)
if err != nil {
    logger.ErrorContext(ctx, "operation failed",
        clog.Error(err),
        clog.String("operation", "DoSomething"),
    )
    return xerrors.Wrap(err, "service.DoSomething")
}
```

## 5. 使用示例

### 5.1 错误包装

```go
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, xerrors.Wrapf(err, "read config file %s", path)
    }
    // ...
}
```

### 5.2 Sentinel 错误检查

```go
result, err := cache.Get(ctx, key)
if xerrors.Is(err, cache.ErrCacheMiss) {
    // 缓存未命中，从数据库加载
}
```

### 5.3 初始化时使用 Must

```go
func main() {
    cfg := xerrors.Must(config.Load("config.yaml"))
    logger := xerrors.Must(clog.New(&cfg.Log))
    redisConn := xerrors.Must(connector.NewRedis(&cfg.Redis))
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
        if xerrors.Is(err, sql.ErrNoRows) {
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
| 组件错误 | 定义 Sentinel Errors 在组件的 `errors.go` 文件 |

## 7. 注意事项

1. **Must 仅用于初始化**：在运行时业务逻辑中使用 `Must` 会导致服务 panic
2. **不在 xerrors 中记录日志**：日志记录是调用方的职责
3. **保持错误链**：使用 `%w` 动词或 `Wrap` 函数，确保 `errors.Is/As` 可用
4. **组件定义自己的错误**：每个组件在 `errors.go` 中定义自己的 Sentinel Errors

## 8. 零依赖设计的意义

xerrors 是 Genesis 唯一**零依赖**的包（除了 Go 标准库），这是刻意的架构决策：

### 8.1 为什么 xerrors 不依赖 clog？

**常见误区**：既然 xerrors 处理错误，为什么不在其中集成日志？

**设计原因**：
1. **职责分离** - 错误创建与错误记录是两个独立的关切点
   - xerrors：创建、包装、检查错误（业务逻辑）
   - clog：记录错误的上下文和堆栈（基础设施）

2. **避免循环依赖** - 如果 xerrors 依赖 clog，而 clog 又使用 xerrors，会形成循环
   ```text
   clog 需要 xerrors：
   - logger.Error(err) 时，clog 需要理解错误类型
   - clog.Error(err) 应该能检查 xerrors.Is(err, ...)
   
   如果 xerrors 又反向依赖 clog，就会循环依赖
   ```

3. **灵活性** - 错误处理与日志记录可以独立演进
   - xerrors 可以在任何 Go 项目中使用，无需 Genesis
   - clog 可以选择不同的 xerrors 版本，或自己的错误类型

### 8.2 正确的错误处理与日志记录模式

```go
// ✅ Good: 分离职责
func LoadConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        // xerrors: 创建/包装错误，不记录日志
        return nil, xerrors.Wrapf(err, "read config file %s", path)
    }
    return parseConfig(data), nil
}

// 调用方处理错误并记录
func main() {
    cfg, err := LoadConfig("config.yaml")
    if err != nil {
        // clog: 记录错误和上下文
        logger.ErrorContext(ctx, "failed to load config",
            clog.Error(err),
            clog.String("file", "config.yaml"),
        )
        return
    }
}

// ❌ Bad: 在 xerrors 中记录日志（违反职责分离）
func Wrap(err error, msg string) error {
    log.Printf("ERROR: %s: %v", msg, err)  // 不应该这样！
    return fmt.Errorf("%s: %w", msg, err)
}
```

### 8.3 与其他 L0 组件的关系

```text
┌────────────────────────────────────┐
│      L0 Foundation Packages        │
├────────────────────────────────────┤
│                                    │
│  xerrors (零依赖)                  │
│    ↓ 被其他所有包使用               │
│                                    │
│  config ─→ 使用 xerrors            │
│  clog ─→ 使用 xerrors              │
│  telemetry ─→ 使用 xerrors         │
│                                    │
│  L1 Connectors ─→ 使用 xerrors     │
│  L2 Components ─→ 使用 xerrors     │
│  L3 Governance ─→ 使用 xerrors     │
│                                    │
└────────────────────────────────────┘
```

## 9. 与框架其他组件的集成

### 9.1 Connector 中的错误处理

Connector 作为 L1 基础设施层，应该定义自己的 Sentinel Errors：

```go
// pkg/connector/errors.go
var (
    // 连接状态错误
    ErrNotConnected  = xerrors.New("connector: not connected")
    ErrAlreadyClosed = xerrors.New("connector: already closed")
    
    // 健康检查错误
    ErrHealthCheckFailed = xerrors.New("connector: health check failed")
)

// 使用时包装基础错误
func (c *redisConnector) Connect(ctx context.Context) error {
    if err := c.client.Ping(ctx).Err(); err != nil {
        return xerrors.Wrapf(err, "redis ping failed: %s", c.cfg.Addr)
    }
    return nil
}
```

### 9.2 业务组件中的错误处理

L2 业务组件应该定义自己的领域特定错误：

```go
// pkg/cache/errors.go
var (
    ErrCacheMiss    = xerrors.New("cache: miss")
    ErrKeyTooLong   = xerrors.New("cache: key too long")
    ErrSerialization = xerrors.New("cache: serialization failed")
)

// pkg/dlock/errors.go
var (
    ErrLockNotHeld   = xerrors.New("dlock: lock not held")
    ErrLockTimeout   = xerrors.New("dlock: acquire timeout")
    ErrAlreadyLocked = xerrors.New("dlock: already locked")
)
```

### 9.3 在 main.go 中使用 Must 进行初始化

遵循 `refactoring-plan.md` 中的 Go Native DI 模式，在初始化时使用 `Must`：

```go
// main.go
package main

import (
    "github.com/ceyewan/genesis/pkg/config"
    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/xerrors"
)

func main() {
    // 初始化阶段：使用 Must 确保关键依赖成功加载
    cfg := xerrors.Must(config.Load("config.yaml"))
    logger := xerrors.Must(clog.New(&cfg.Log))
    redisConn := xerrors.Must(connector.NewRedis(&cfg.Redis))
    defer redisConn.Close()
    
    mysqlConn := xerrors.Must(connector.NewMySQL(&cfg.MySQL))
    defer mysqlConn.Close()
    
    // 后续服务启动...
}
```

**为什么在初始化时使用 Must？**
- 初始化失败应该快速失败，而非通过 main 返回
- 如果配置、数据库连接失败，服务无法启动，panic 是正确的行为
- 在运行时业务逻辑中绝不应该使用 Must

## 10. 完整使用示例

参见 `examples/xerrors/main.go` 获取包含以下场景的完整可运行示例：

1. **基础错误包装** - Wrap/Wrapf 的多层链式调用
2. **Sentinel Errors 检查** - 各种 Sentinel Errors 的判断和提取
3. **带错误码的错误** - WithCode/GetCode 用于 API 错误映射
4. **错误收集** - Collector 用于表单验证等场景
5. **多错误合并** - Combine 用于聚合多个操作的错误
6. **初始化时 Must** - 展示 Must 的正确使用场景
7. **实战 API 场景** - 模拟真实的 HTTP API 错误处理流程

运行示例：
```bash
cd examples/xerrors
go run main.go
```
