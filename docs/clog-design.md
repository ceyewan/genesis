# clog 日志库设计文档

## 1. 目标与原则

`clog` 旨在提供一个基于 `slog` 的、简洁、易用且高度抽象的日志库，用于替代现有直接暴露底层实现的日志方案。

**设计原则：**
1. **抽象接口：** 永不暴露底层日志库的类型。
2. **简洁 API：** 统一使用 `Config + Option` 进行配置。
3. **标准化：** 统一错误字段、Context 字段和命名空间结构。
4. **去冗余：** 默认不内置文件轮转逻辑，鼓励使用外部收集器（如 K8s/Loki/Fluentd）。
5. **层级命名空间：** 支持递归扩展命名空间，便于微服务架构中的组件标识。

## 2. 项目结构

遵循 Go 标准项目布局：

```
genesis/
├── pkg/
│   └── clog/                    # 公开API入口
│       ├── clog.go             # 工厂函数和便利函数
│       └── types/              # 类型定义（接口、结构体）
│           ├── logger.go       # Logger接口定义
│           ├── config.go       # Config和Option结构体
│           ├── fields.go       # Field类型和构造函数
│           └── level.go        # 日志级别
├── internal/
│   └── clog/                   # 内部实现
│       ├── logger.go           # Logger接口实现
│       ├── builder.go          # LogBuilder实现
│       ├── context.go          # Context字段提取
│       ├── namespace.go        # 命名空间处理
│       └── slog/               # slog适配器
│           ├── handler.go      
│           └── formatter.go    
├── examples/
└── ...
```

依赖关系图：
```用户代码
    ↓ import
pkg/clog (工厂函数 + 重新导出)
    ↓ import
pkg/clog/types (接口定义)
    ↑ implement
internal/clog (具体实现)
    ↓ import  
internal/clog/slog (slog适配器)
```

## 3. 核心 API 设计

核心 API 位于 `pkg/clog/` 包中。

### 3.1. 字段抽象

使用 `Field` 抽象函数来构建日志字段，避免直接暴露底层类型。

```go
// pkg/clog/api.go
package clog

import "context"

// Field 是用于构建日志字段的抽象类型。
type Field func(*LogBuilder)

// LogBuilder 用于在日志记录前收集和处理所有字段。
type LogBuilder struct {
    // 内部数据结构，用于收集键值对
    data map[string]any 
}
```

### 3.2. Logger 接口

`Logger` 接口定义了所有日志操作，包括带 Context 和不带 Context 的方法，以及链式操作。

```go
// pkg/clog/api.go
type Logger interface {
    // 基础日志级别方法
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)

    // 带 Context 的日志级别方法，用于自动提取 Context 字段
    DebugContext(ctx context.Context, msg string, fields ...Field)
    InfoContext(ctx context.Context, msg string, fields ...Field)
    WarnContext(ctx context.Context, msg string, fields ...Field)
    ErrorContext(ctx context.Context, msg string, fields ...Field)
    FatalContext(ctx context.Context, msg string, fields ...Field)

    // With 创建一个带有预设字段的子 Logger
    With(fields ...Field) Logger
    
    // WithNamespace 创建一个扩展命名空间的子 Logger
    WithNamespace(parts ...string) Logger
    
    // SetLevel 动态调整日志级别
    SetLevel(level Level) error
    
    // Flush 强制同步所有缓冲区的日志
    Flush()
}
```

### 3.3. 日志级别

```go
// pkg/clog/level.go
type Level int

const (
    DebugLevel Level = iota - 4
    InfoLevel
    WarnLevel
    ErrorLevel
    FatalLevel
)
```

## 4. 配置与选项设计

配置分为全局配置 `Config` 和实例选项 `Option`。

### 4.1. Config (全局配置)

```go
// pkg/clog/config.go
type Config struct {
    Level       string `json:"level" yaml:"level"`         // debug|info|warn|error|fatal
    Format      string `json:"format" yaml:"format"`       // json|console
    Output      string `json:"output" yaml:"output"`       // stdout|stderr|<file path>
    EnableColor bool   `json:"enableColor" yaml:"enableColor"`
    AddSource   bool   `json:"addSource" yaml:"addSource"`
    SourceRoot  string `json:"sourceRoot" yaml:"sourceRoot"` // 用于裁剪文件路径
}

func (c *Config) Validate() error { /* ... */ }
```

### 4.2. Option (实例选项)

`Option` 主要用于配置命名空间和 Context 字段提取规则。

```go
// pkg/clog/config.go
type ContextField struct {
    Key         any         // Context 中存储的键
    FieldName   string      // 输出的最终字段名，如 "ctx.trace_id"
    Required    bool        // 是否必须存在
    Extract     func(any) (any, bool) // 可选的自定义提取函数
}

type Option struct {
    NamespaceParts    []string        // 多级命名空间，如 ["order-service", "handler"]
    ContextFields     []ContextField  // Context 字段提取规则
    ContextPrefix     string          // Context 字段前缀，默认 "ctx."
    NamespaceJoiner   string          // 命名空间连接符，默认 "."
}
```

## 5. 字段构造函数标准化

所有字段构造函数位于 `pkg/clog/fields.go`。

### 5.1. 基础类型

提供所有基础类型的构造函数，返回 `Field`。

```go
func String(k, v string) Field
func Int(k string, v int) Field
func Int64(k string, v int64) Field
func Float64(k string, v float64) Field
func Bool(k string, v bool) Field
func Duration(k string, v time.Duration) Field
func Time(k string, v time.Time) Field
func Any(k string, v any) Field
```

### 5.2. 错误处理标准化

错误字段统一拆解为结构化字段，便于检索和分析。

| 字段名 | 描述 |
|---|---|
| `err_msg` | 错误消息 (Error()) |
| `err_type` | 错误类型 (fmt.Sprintf("%T", err)) |
| `err_stack` | 错误堆栈（包含文件名、行号、函数名） |
| `err_code` | 可选的业务错误码 |

```go
func Error(err error) Field
func ErrorWithCode(err error, code string) Field
```

错误堆栈信息示例：
```json
{
  "err_msg": "database connection failed",
  "err_type": "*errors.errorString",
  "err_stack": "/Users/ceyewan/CodeField/genesis/internal/clog/logger.go:52 github.com/ceyewan/genesis/internal/clog.(*loggerImpl).Error\n/Users/ceyewan/CodeField/genesis/examples/clog-basic/main.go:189 main.basicErrorExample\n..."
}
```

### 5.3. 常用语义字段

提供常用语义字段的别名，确保字段名统一。

```go
func RequestID(id string) Field          // 映射到 request_id
func UserID(id string) Field             // 映射到 user_id
func TraceID(id string) Field            // 映射到 trace_id
func Component(name string) Field        // 映射到 component
```

## 6. 命名空间设计

### 6.1. 层级命名空间

支持递归扩展命名空间，适用于微服务架构：

- **输入：** `Option.NamespaceParts` (e.g., `["user-service", "repo"]`)
- **扩展：** `logger.WithNamespace("cache")` 
- **输出：**
    - `namespace`: 拼接后的字符串 (e.g., `"user-service.repo.cache"`)

### 6.2. 使用示例

```go
// 主服务创建 logger
mainLogger := clog.New(config, &clog.Option{
    NamespaceParts: []string{"user-service"},
})

// 分布式锁组件扩展命名空间
lockLogger := mainLogger.WithNamespace("lock")
// 输出: namespace="user-service.lock"

// 更深层级
redisLockLogger := lockLogger.WithNamespace("redis")
// 输出: namespace="user-service.lock.redis"
```

## 7. Context 集成

- **前缀隔离：** 所有自动提取的 Context 字段默认添加 `ctx.` 前缀，避免与业务字段冲突。
- **配置化提取：** 通过 `Option.ContextFields` 定义提取规则，支持自定义 Key 和提取函数。

## 8. Source 路径裁剪

- **SourceRoot：** 配置一个根路径，用于从文件路径中裁剪掉公共前缀，使控制台输出更简洁。
- **简化实现：** 仅支持基于 `SourceRoot` 的简单路径裁剪。

## 9. 工厂函数

工厂函数位于 `pkg/clog/factory.go`，负责初始化底层 `slog` 库。

```go
// pkg/clog/factory.go
package clog

func New(config *Config, option *Option) (Logger, error) {
    // 1. 验证配置
    // 2. 初始化底层 slog handler
    // 3. 封装成 clog.Logger 接口实现
}

func Default() Logger {
    // 返回默认配置的Logger
}
```

## 10. 完整使用示例

### 10.1. 基础使用

```go
package main

import "github.com/genesis/pkg/clog"

func main() {
    logger := clog.New(&clog.Config{
        Level:  "info",
        Format: "json",
        Output: "stdout",
    }, &clog.Option{
        NamespaceParts: []string{"user-service"},
    })
    
    logger.Info("service starting", 
        clog.String("version", "v1.0.0"),
        clog.Int("port", 8080))
}
```

### 10.2. 微服务架构使用

```go
// main.go - 主服务
func main() {
    logger := clog.New(config, &clog.Option{
        NamespaceParts: []string{"user-service"},
    })
    
    // 传给各组件，自动继承并扩展命名空间
    lockService := NewDistributedLock(logger.WithNamespace("lock"))
    userRepo := NewUserRepo(logger.WithNamespace("repo", "user"))
}

// lock.go - 分布式锁组件
func NewDistributedLock(logger clog.Logger) *DistributedLock {
    return &DistributedLock{logger: logger}
}

func (d *DistributedLock) Lock(key string) error {
    // 日志输出: namespace="user-service.lock"
    d.logger.Info("attempting to acquire lock", clog.String("key", key))
    
    // 进一步扩展命名空间
    redisLogger := d.logger.WithNamespace("redis")
    // 日志输出: namespace="user-service.lock.redis"
    redisLogger.Debug("sending redis command", clog.String("cmd", "SET"))
    
    return nil
}
```

## 11. 技术实现细节

- **底层库：** 基于 Go 标准库 `log/slog`
- **性能：** 利用 `slog` 的高性能特性和延迟计算
- **扩展性：** 通过 `internal/clog/slog/` 包封装，便于未来切换底层实现
- **内存管理：** 复用 `LogBuilder` 对象，减少内存分配

## 12. 最佳实践建议

### 12.1. 环境配置建议

根据不同的部署环境，建议使用不同的日志配置：

#### 开发环境
```go
logger := clog.New(&clog.Config{
    Level:       "debug",           // 显示所有日志级别
    Format:      "console",         // 便于阅读的格式
    Output:      "stdout",          // 输出到控制台
    EnableColor: true,              // 启用颜色支持
    AddSource:   true,              // 显示源码位置
}, nil)
```

#### 测试环境
```go
logger := clog.New(&clog.Config{
    Level:       "info",            // 显示info及以上级别
    Format:      "json",            // 便于机器解析的格式
    Output:      "stdout",          // 输出到控制台
    EnableColor: false,             // 禁用颜色
    AddSource:   true,              // 显示源码位置
}, nil)
```

#### 生产环境
```go
logger := clog.New(&clog.Config{
    Level:       "warn",            // 只显示warn及以上级别
    Format:      "json",            // 便于机器解析的格式
    Output:      "stdout",          // 输出到标准输出（由日志收集器处理）
    EnableColor: false,             // 禁用颜色
    AddSource:   true,              // 显示源码位置
    SourceRoot:  "genesis",         // 裁剪路径，从genesis开始显示
}, nil)
```

### 12.2. 命名空间 vs 组件使用规范

明确区分 `namespace` 和 `component` 的使用场景：

#### Namespace（业务逻辑层级）
- **用途：** 表示代码的业务逻辑层级和调用链
- **示例：** `user-service.handler.auth`
- **场景：** 微服务架构中的服务分层

```go
// 主服务
mainLogger := clog.New(config, &clog.Option{
    NamespaceParts: []string{"user-service"},
})

// Handler层
handlerLogger := mainLogger.WithNamespace("handler")
authLogger := handlerLogger.WithNamespace("auth") // namespace="user-service.handler.auth"
```

#### Component（技术组件）
- **用途：** 标识使用的具体技术组件
- **示例：** `database`, `redis`, `message-queue`
- **场景：** 技术架构中的组件标识

```go
logger.Info("querying user data",
    clog.Component("database"),    // 技术组件
    clog.String("table", "users"),
    clog.String("operation", "SELECT"))
```

### 12.3. 错误处理最佳实践

使用结构化的错误字段，包含完整的错误信息：

```go
// 简单错误
err := errors.New("database connection failed")
logger.Error("operation failed", clog.Error(err))
// 输出: {"err_msg":"database connection failed","err_type":"*errors.errorString","err_stack":"..."}

// 带错误码的业务错误
logger.Error("business logic error",
    clog.ErrorWithCode(err, "DB_CONN_001"),
    clog.String("operation", "user_query"))
// 输出: {"err_msg":"...","err_type":"...","err_code":"DB_CONN_001","err_stack":"..."}
```

### 12.4. Context 字段使用规范

使用 Context 传递请求级别的元数据：

```go
// 配置Context字段提取
logger := clog.New(config, &clog.Option{
    ContextFields: []clog.ContextField{
        {Key: "request_id", FieldName: "request_id", Required: false},
        {Key: "user_id", FieldName: "user_id", Required: false},
        {Key: "trace_id", FieldName: "trace_id", Required: false},
    },
    ContextPrefix: "ctx.",
})

// 在请求开始时设置Context值
ctx := context.WithValue(context.Background(), "request_id", "req-12345")
ctx = context.WithValue(ctx, "user_id", "user-67890")

// 使用Context方法自动提取字段
logger.InfoContext(ctx, "user login successful",
    clog.String("method", "password"))
// 输出: {"ctx.request_id":"req-12345","ctx.user_id":"user-67890","method":"password"}
```

### 12.5. 时间戳格式统一

所有日志统一使用 ISO8601 格式（RFC3339）：
- 格式：`2006-01-02T15:04:05.000Z`
- 示例：`2025-11-18T18:40:38+08:00`
- 优势：国际化、机器可读、包含时区信息

### 12.6. 日志级别控制

确保日志级别过滤正确工作：
- **默认级别：** INFO（不输出DEBUG日志）
- **级别映射：** 正确映射自定义级别到slog级别
- **动态调整：** 支持运行时调整日志级别

### 12.7. 路径显示规范

根据环境需要选择合适的路径显示方式：

```go
// 开发环境：显示完整绝对路径
{"caller":"/Users/ceyewan/CodeField/genesis/examples/clog-basic/main.go:64"}

// 生产环境：显示相对路径（从genesis开始）
{"caller":"genesis/examples/clog-basic/main.go:64"}
```

### 12.8. 性能考虑

- **延迟计算：** 利用slog的延迟计算特性
- **对象复用：** 复用LogBuilder对象，减少内存分配
- **级别过滤：** 在底层进行级别过滤，避免不必要的字段处理