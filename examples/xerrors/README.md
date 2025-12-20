# xerrors 示例

本示例演示 Genesis `xerrors` 包的各种用法和最佳实践。

## 快速开始

运行示例：

```bash
cd examples/xerrors
go run main.go
```

## 示例内容

### 1. 基础错误包装 (Wrap/Wrapf)

演示如何使用上下文信息包装错误，形成可读的错误链。

```go
baseErr := errors.New("connection refused")
wrapped := xerrors.Wrap(baseErr, "failed to connect to database")
wrapped = xerrors.Wrapf(wrapped, "host: %s, port: %d", "localhost", 5432)
```

**输出**: `host: localhost, port: 5432: failed to connect to database: connection refused`

### 2. Sentinel Errors 检查

演示如何使用 `errors.Is()` 检查特定的错误类型，支持多层包装的错误链。

```go
err := xerrors.Wrap(xerrors.ErrNotFound, "user not found")
if xerrors.Is(err, xerrors.ErrNotFound) {
    // 处理未找到的情况
}
```

可用的 Sentinel Errors：
- `ErrNotFound` (404)
- `ErrAlreadyExists` (409)
- `ErrInvalidInput` (400)
- `ErrTimeout` (504)
- `ErrUnavailable` (503)
- `ErrUnauthorized` (401)
- `ErrForbidden` (403)
- `ErrConflict` (409)
- `ErrInternal` (500)
- `ErrCanceled` (499)

### 3. 带错误码的错误 (WithCode/GetCode)

演示如何为错误添加机器可读的错误码，便于 API 错误映射。

```go
codedErr := xerrors.WithCode(cacheErr, "CACHE_MISS")
code := xerrors.GetCode(codedErr)
// code = "CACHE_MISS"
```

### 4. 错误收集 (Collector)

演示如何收集多个操作的错误，保留第一个错误。适用于表单验证等场景。

```go
var collector xerrors.Collector
collector.Collect(validateName(u.Name))
collector.Collect(validateEmail(u.Email))
collector.Collect(validateAge(u.Age))
return collector.Err()  // 返回第一个错误或 nil
```

### 5. 多个错误合并 (Combine)

演示如何合并多个错误为一个，支持 `errors.Is()` 检查。

```go
err1 := errors.New("database error")
err2 := errors.New("cache error")
err3 := errors.New("validation error")
combined := xerrors.Combine(err1, err2, err3)

if xerrors.Is(combined, err2) {
    // 检查到 cache error
}
```

### 6. 初始化时使用 Must

演示 `Must` 函数的正确用法（仅在初始化阶段使用）。

```go
func main() {
    // ✅ 在初始化时使用 Must
    cfg := xerrors.Must(config.Load("config.yaml"))
    logger := xerrors.Must(clog.New(&cfg.Log))
    conn := xerrors.Must(connector.NewRedis(&cfg.Redis))
    defer conn.Close()
}
```

**重要**: `Must` 仅应在初始化阶段使用，运行时使用会导致 panic。

### 7. 实战场景 - API 错误处理

演示在实际 API 场景中如何处理错误，包括 HTTP 状态码映射。

```go
user, err := getUserFromDB(userID)
if err != nil {
    if xerrors.Is(err, xerrors.ErrNotFound) {
        return HTTP 404 Not Found
    }
    if xerrors.Is(err, xerrors.ErrTimeout) {
        return HTTP 503 Service Unavailable
    }
    return HTTP 500 Internal Server Error
}
```

## 最佳实践

### ✅ 应该做

```go
// 1. 在每层都包装错误
func readConfig(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, xerrors.Wrapf(err, "read config from %s", path)
    }
    return parseConfig(data), nil
}

// 2. 使用 Sentinel Errors 进行类型检查
func main() {
    cfg, err := readConfig("config.yaml")
    if err != nil {
        if xerrors.Is(err, xerrors.ErrNotFound) {
            logger.Error("config file not found")
        }
        return
    }
}

// 3. 在初始化时使用 Must
func init() {
    logger = xerrors.Must(clog.New(&cfg.Log))
}

// 4. 使用 Collector 进行多步骤验证
var errs xerrors.Collector
errs.Collect(validateField1(v1))
errs.Collect(validateField2(v2))
return errs.Err()
```

### ❌ 不应该做

```go
// 1. 不要在初始化之外使用 Must
func handler(req *Request) error {
    user := xerrors.Must(db.FindUser(req.UserID))  // ❌ 错误！
    return nil
}

// 2. 不要在 xerrors 中记录日志
// xerrors 是底层库，日志记录应该由调用方决定

// 3. 不要创建没有上下文的错误链
return errors.New("failed")  // ❌ 不清楚失败原因
return xerrors.Wrap(err, "failed")  // ✅ 清楚失败原因
```

## 与其他组件的集成

### 与 clog 配合

```go
import (
    "github.com/ceyewan/genesis/xerrors"
    "github.com/ceyewan/genesis/clog"
)

result, err := service.DoSomething(ctx)
if err != nil {
    // xerrors: 包装错误
    err = xerrors.Wrap(err, "service.DoSomething")
    
    // clog: 记录错误
    logger.ErrorContext(ctx, "operation failed",
        clog.Error(err),
        clog.String("operation", "DoSomething"),
    )
    return nil, err
}
```

### 在 Connector 中使用

```go
// pkg/connector/errors.go
var (
    ErrNotConnected = xerrors.New("connector: not connected")
    ErrAlreadyClosed = xerrors.New("connector: already closed")
)

// pkg/connector/redis.go
func (c *redisConnector) Connect(ctx context.Context) error {
    if err := c.client.Ping(ctx).Err(); err != nil {
        return xerrors.Wrapf(err, "redis ping failed: %s", c.cfg.Addr)
    }
    return nil
}
```

### 在业务组件中使用

```go
// pkg/cache/errors.go
var (
    ErrCacheMiss = xerrors.New("cache: miss")
    ErrKeyTooLong = xerrors.New("cache: key too long")
)

// pkg/dlock/errors.go
var (
    ErrLockNotHeld = xerrors.New("dlock: lock not held")
    ErrLockTimeout = xerrors.New("dlock: acquire timeout")
)
```

## 参考文档

- [xerrors 设计文档](../../docs/foundation/xerrors-design.md)
- [Go 错误处理最佳实践](https://pkg.go.dev/errors)
