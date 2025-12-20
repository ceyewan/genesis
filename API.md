# Genesis API 文档

本文档提供 Genesis 微服务组件库的完整 API 使用指南。Genesis 是一个轻量级、可复用的 Go 微服务组件库，采用四层扁平化架构设计。

> **重要提示**：Genesis 不是框架——我们提供积木，用户自己搭建。

## 目录

- [快速开始](#快速开始)
- [架构概览](#架构概览)
- [核心组件使用指南](#核心组件使用指南)
  - [clog - 结构化日志](#clog---结构化日志)
  - [config - 配置管理](#config---配置管理)
  - [connector - 连接管理](#connector---连接管理)
  - [db - 数据库组件](#db---数据库组件)
  - [dlock - 分布式锁](#dlock---分布式锁)
  - [cache - 缓存组件](#cache---缓存组件)
  - [auth - 身份认证](#auth---身份认证)
- [开发模式](#开发模式)
- [最佳实践](#最佳实践)

## 快速开始

### 安装

```bash
go get github.com/ceyewan/genesis
```

### 基本使用示例

```go
package main

import (
    "context"
    "os/signal"
    "syscall"

    "github.com/ceyewan/genesis/pkg/clog"
    "github.com/ceyewan/genesis/pkg/config"
    "github.com/ceyewan/genesis/pkg/connector"
    "github.com/ceyewan/genesis/pkg/dlock"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    // 1. 加载配置
    cfg, _ := config.Load("config.yaml")

    // 2. 创建 Logger
    logger, _ := clog.New(&cfg.Log)
    defer logger.Flush()

    // 3. 创建连接器（自动资源管理）
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()

    // 4. 使用组件
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock, dlock.WithLogger(logger))

    logger.Info("application started")

    // 使用分布式锁
    if err := locker.Lock(ctx, "my_resource"); err == nil {
        defer locker.Unlock(ctx, "my_resource")
        // 业务逻辑...
        logger.Info("lock acquired successfully")
    }
}
```

## 架构概览

Genesis 采用四层扁平化架构：

| 层次 | 核心组件 | 职责 | 示例组件 |
| :----- | :--------- | :----- | :---------- |
| **Level 3: Governance** | 治理组件 | 流量治理，身份认证，切面能力 | auth, ratelimit, breaker, registry |
| **Level 2: Business** | 业务组件 | 业务能力封装 | cache, idgen, dlock, idempotency, mq |
| **Level 1: Infrastructure** | 基础设施 | 连接管理，底层 I/O | connector, db |
| **Level 0: Base** | 基础组件 | 框架基石 | clog, config, metrics, xerrors |

**设计原则**：

- **显式优于隐式**：使用Go原生构造函数注入
- **简单优于聪明**：避免过度抽象
- **组合优于继承**：通过组合实现功能扩展

## 核心组件使用指南

### clog - 结构化日志

`clog` 是基于 Go 标准库 `slog` 的结构化日志组件，支持命名空间派生和 Context 字段提取。

#### 基本用法

```go
import "github.com/ceyewan/genesis/pkg/clog"

// 创建默认 Logger
logger := clog.Default()

// 基础日志
logger.Info("service started",
    clog.String("version", "v1.0.0"),
    clog.Int("port", 8080))

// 错误日志
logger.Error("operation failed",
    clog.Error(err),
    clog.String("operation", "createUser"))
```

#### 配置选项

```go
config := &clog.Config{
    Level:       "info",              // debug|info|warn|error|fatal
    Format:      "json",              // json|console
    Output:      "stdout",            // stdout|stderr|<file path>
    AddSource:   true,                // 显示源码位置
    SourceRoot:  "genesis",           // 裁剪路径前缀
    EnableColor: false,               // 控制台颜色（仅console格式）
}

logger, err := clog.New(config, nil)
```

#### 高级功能

**命名空间**：

```go
// 应用级 Logger
appLogger := clog.Default().WithNamespace("user-service")

// 组件级 Logger
handlerLogger := appLogger.WithNamespace("handler")
// 输出命名空间: user-service.handler
```

**Context 字段提取**：

```go
option := &clog.Option{
    ContextFields: []clog.ContextField{
        {Key: "request_id", FieldName: "request_id"},
        {Key: "user_id", FieldName: "user_id"},
    },
    ContextPrefix: "ctx.",
}

logger, _ := clog.New(config, option)

// 自动提取 Context 中的字段
logger.InfoContext(ctx, "user login successful")
```

**语义化字段**：

```go
logger.Info("processing request",
    clog.RequestID("req-123456"),
    clog.UserID("user-789"),
    clog.TraceID("trace-abc"),
    clog.Component("database"))
```

**错误处理**：

```go
// 简单错误
logger.Error("database error", clog.Error(err))

// 带错误码的错误
logger.Error("business logic error",
    clog.ErrorWithCode(err, "DB_CONN_001"))
```

> **详细信息**：参阅 [clog 设计文档](docs/foundation/clog-design.md)

### config - 配置管理

`config` 组件提供统一的配置管理能力，支持多源加载（YAML/JSON、环境变量、.env）和热更新。

```go
import "github.com/ceyewan/genesis/pkg/config"

// 快捷方式：创建并加载配置
loader := config.MustLoad(
    config.WithConfigName("config"),
    config.WithConfigPaths(".", "./config"),
)

// 应用配置结构体
type AppConfig struct {
    App struct {
        Name        string `mapstructure:"name"`
        Version     string `mapstructure:"version"`
        Environment string `mapstructure:"environment"`
        Debug       bool   `mapstructure:"debug"`
    } `mapstructure:"app"`

    MySQL struct {
        Host     string `mapstructure:"host"`
        Port     int    `mapstructure:"port"`
        Username string `mapstructure:"username"`
        Database string `mapstructure:"database"`
    } `mapstructure:"mysql"`

    Redis struct {
        Addr     string `mapstructure:"addr"`
        Password string `mapstructure:"password"`
        DB       int    `mapstructure:"db"`
    } `mapstructure:"redis"`

    Log clog.Config `mapstructure:"clog"`
}

// 解析配置到结构体
var cfg AppConfig
if err := loader.Unmarshal(&cfg); err != nil {
    panic(err)
}

// 使用配置
logger, _ := clog.New(&cfg.Log)
redisConn, _ := connector.NewRedis(&cfg.Redis)
```

#### 高级功能

**多源加载与优先级**：
```go
// 配置加载优先级（从高到低）：
// 1. 环境变量 (GENESIS_*)
// 2. .env 文件
// 3. 环境特定配置 (config.{env}.yaml)
// 4. 基础配置 (config.yaml)

loader, _ := config.New(
    config.WithConfigName("config"),
    config.WithConfigPaths("./config", "/etc/myapp"),
    config.WithEnvPrefix("MYAPP"),
)
loader.Load(context.Background())
```

**部分解析**：
```go
// 只解析 MySQL 配置
var mysqlConfig struct {
    Host     string `mapstructure:"host"`
    Port     int    `mapstructure:"port"`
    Database string `mapstructure:"database"`
}
loader.UnmarshalKey("mysql", &mysqlConfig)
```

**配置监听和热更新**：
```go
// 监听配置变化
ch, _ := loader.Watch(ctx, "mysql.host")
go func() {
    for event := range ch {
        logger.Info("config changed",
            clog.String("key", event.Key),
            clog.Any("old_value", event.OldValue),
            clog.Any("new_value", event.Value),
        )
        // 重新初始化相关组件...
    }
}()
```

**配置选项**：
```go
loader, _ := config.New(
    config.WithConfigName("app"),           // 配置文件名（不带扩展名）
    config.WithConfigPaths(".", "./config"), // 搜索路径
    config.WithConfigType("yaml"),           // 文件类型
    config.WithEnvPrefix("MYAPP"),           // 环境变量前缀
)
```

**配置文件示例**：

```yaml
# config.yaml - 基础配置
app:
  name: "my-service"
  version: "1.0.0"
  environment: "production"
  debug: false

mysql:
  host: "localhost"
  port: 3306
  username: "user"
  password: "pass"
  database: "mydb"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

clog:
  level: "info"
  format: "json"
  output: "stdout"
  addSource: true
```

```yaml
# config.dev.yaml - 开发环境覆盖
app:
  debug: true

mysql:
  host: "dev-mysql.internal"

redis:
  addr: "dev-redis.internal:6379"
```

```bash
# .env 文件 - 本地环境变量
MYAPP_MYSQL_PASSWORD=dev_password
MYAPP_REDIS_PASSWORD=dev_redis_pass
MYAPP_CLOG_LEVEL=debug
```

**环境变量规则**：
- 前缀：默认为 `GENESIS`（可通过 `WithEnvPrefix` 修改）
- 分隔符：使用下划线 `_` 替代层级点 `.`
- 格式：`{PREFIX}_{SECTION}_{KEY}`（全大写）

示例：YAML `mysql.host` → 环境变量 `GENESIS_MYSQL_HOST`

> **详细信息**：参阅 [config 设计文档](docs/foundation/config-design.md)

### xerrors - 错误处理

`xerrors` 是 Genesis 的统一错误处理组件，提供标准化的错误创建、包装和检查能力。

```go
import "github.com/ceyewan/genesis/pkg/xerrors"
```

#### Sentinel Errors

预定义的通用错误类型，用于快速错误检查：

```go
// 内置 Sentinel Errors
var (
    ErrNotFound      = xerrors.New("not found")
    ErrAlreadyExists = xerrors.New("already exists")
    ErrInvalidInput  = xerrors.New("invalid input")
    ErrTimeout       = xerrors.New("timeout")
    ErrUnavailable   = xerrors.New("unavailable")
    ErrUnauthorized  = xerrors.New("unauthorized")
    ErrForbidden     = xerrors.New("forbidden")
    ErrConflict      = xerrors.New("conflict")
    ErrInternal      = xerrors.New("internal error")
    ErrCanceled      = xerrors.New("canceled")
)

// 使用示例
result, err := cache.Get(ctx, key)
if xerrors.Is(err, cache.ErrCacheMiss) {
    // 处理缓存未命中
}
```

#### 错误包装

为错误添加上下文信息，同时保留错误链：

```go
// 基础包装
err := xerrors.Wrap(dbErr, "database query failed")

// 格式化包装
err := xerrors.Wrapf(dbErr, "query user %d", userID)

// 添加错误码
err := xerrors.WithCode(xerrors.ErrNotFound, "USER_NOT_FOUND")

// 错误检查
if xerrors.Is(err, xerrors.ErrNotFound) {
    // 处理未找到的情况
}
```

#### 带错误码的错误

用于 API 错误响应和监控告警：

```go
// 创建带码错误
err := xerrors.WithCode(dbErr, "DB_QUERY_FAILED")

// 从错误链中提取错误码
code := xerrors.GetCode(err)
if code != "" {
    // 根据错误码返回不同的 HTTP 状态码
    return HTTPError{
        Code:    code,
        Message: err.Error(),
        Status:  codeToHTTPStatus(code),
    }
}
```

#### 错误收集与合并

```go
// Collector - 收集多个错误，只保留第一个
var errs xerrors.Collector
errs.Collect(validateName(name))
errs.Collect(validateEmail(email))
errs.Collect(validateAge(age))
if err := errs.Err(); err != nil {
    return err
}

// Combine - 合并多个错误
err1 := operation1()
err2 := operation2()
err3 := operation3()
combined := xerrors.Combine(err1, err2, err3)

// MultiError 支持 errors.Is 检查
if xerrors.Is(combined, someExpectedError) {
    // 某个预期的错误在合并的错误中
}
```

#### 初始化时的 Must 函数

仅用于应用初始化阶段，遇到错误时 panic：

```go
func main() {
    // 仅在初始化阶段使用 Must
    cfg := xerrors.Must(config.Load("config.yaml"))
    logger := xerrors.Must(clog.New(&cfg.Log))
    redisConn := xerrors.Must(connector.NewRedis(&cfg.Redis))
    defer redisConn.Close()

    // 业务逻辑启动...
}
```

#### 标准库函数再导出

```go
// xerrors 重新导出标准库 errors 包的函数
var (
    New    = xerrors.New    // = errors.New
    Is     = xerrors.Is     // = errors.Is
    As     = xerrors.As     // = errors.As
    Unwrap = xerrors.Unwrap // = errors.Unwrap
    Join   = xerrors.Join   // = errors.Join
)
```

#### 最佳实践

| 场景 | 推荐做法 |
|-----|---------|
| 业务逻辑错误 | `if err != nil { return xerrors.Wrap(err, "context") }` |
| 初始化阶段 | `cfg := xerrors.Must(load())` |
| 多步骤验证 | 使用 `Collector` 或 `Combine` |
| API 错误响应 | 使用 `WithCode` 添加机器可读码 |
| 日志记录 | 在调用方使用 `clog.Error(err)` |

> **详细信息**：参阅 [xerrors 设计文档](docs/foundation/xerrors-design.md)

### connector - 连接管理

`connector` 是 Genesis 基础设施层的核心组件，负责管理与外部服务（MySQL、Redis、Etcd、NATS）的原始连接。提供类型安全的客户端访问、健康检查与连接监控。

```go
import "github.com/ceyewan/genesis/pkg/connector"

// Redis 连接
redisConn, err := connector.NewRedis(&connector.RedisConfig{
    Addr:         "localhost:6379",
    Password:     "",
    DB:           0,
    PoolSize:     10,
    MinIdleConns: 5,
}, connector.WithLogger(logger))
if err != nil {
    log.Fatal(err)
}
defer redisConn.Close()

// 建立连接
if err := redisConn.Connect(ctx); err != nil {
    log.Fatal(err)
}

// 获取客户端并进行操作
client := redisConn.GetClient()
client.Set(ctx, "key", "value", time.Minute)
```

**支持的服务类型**：

#### Redis 连接
```go
redisConn, err := connector.NewRedis(&connector.RedisConfig{
    Addr:         "localhost:6379",
    Password:     "",
    DB:           0,
    PoolSize:     10,
    MinIdleConns: 5,
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
}, connector.WithLogger(logger))
```

#### MySQL 连接
```go
mysqlConn, err := connector.NewMySQL(&connector.MySQLConfig{
    Host:            "localhost",
    Port:            3306,
    Username:        "user",
    Password:        "password",
    Database:        "mydb",
    Charset:         "utf8mb4",
    MaxIdleConns:    10,
    MaxOpenConns:    100,
    ConnMaxLifetime: time.Hour,
}, connector.WithLogger(logger))

// 获取 GORM 实例
db := mysqlConn.GetClient()
var user User
db.First(&user, 1)
```

#### Etcd 连接
```go
etcdConn, err := connector.NewEtcd(&connector.EtcdConfig{
    Endpoints:        []string{"localhost:2379"},
    Username:         "",
    Password:         "",
    DialTimeout:      5 * time.Second,
    KeepAliveTime:    10 * time.Second,
    KeepAliveTimeout: 3 * time.Second,
}, connector.WithLogger(logger))

// 获取 etcd 客户端
client := etcdConn.GetClient()
resp, _ := client.Get(ctx, "/mykey")
```

#### NATS 连接
```go
natsConn, err := connector.NewNATS(&connector.NATSConfig{
    URL:           "nats://localhost:4222",
    Name:          "my-client",
    MaxReconnects: 60,
    ReconnectWait: 2 * time.Second,
}, connector.WithLogger(logger))

// 获取 NATS 连接
nc := natsConn.GetClient()
nc.Publish("subject", []byte("message"))
```

**核心接口方法**：

- `Connect(ctx)` - 建立连接（幂等且并发安全）
- `Close()` - 关闭连接，释放资源
- `HealthCheck(ctx)` - 检查连接健康状态
- `IsHealthy()` - 返回缓存的健康状态标志
- `Name()` - 返回连接实例名称
- `GetClient()` - 获取类型安全的底层客户端

**配置选项**：
```go
// 通用配置（所有连接器都支持）
type BaseConfig struct {
    Name            string        // 连接器名称
    MaxRetries      int           // 最大重试次数
    RetryInterval   time.Duration // 重试间隔
    ConnectTimeout  time.Duration // 连接超时
    HealthCheckFreq time.Duration // 健康检查频率
}

// 注入选项
connector.WithLogger(logger)  // 注入日志器
connector.WithMeter(meter)    // 注入指标收集
```

**资源所有权原则**：
- Connector 拥有连接，必须调用 `Close()` 释放
- 组件（如 cache、dlock）借用 Connector，无需关闭
- 使用 Go 的 `defer` 自然管理资源生命周期

> **详细信息**：参阅 [connector 设计文档](docs/infrastructure/connector-design.md)

### db - 数据库组件

基于 GORM 的数据库组件，支持分库分表和统一事务接口。

```go
import "github.com/ceyewan/genesis/pkg/db"

database, err := db.New(mysqlConn, &cfg.DB,
    db.WithLogger(logger),
    db.WithMeter(meter),
)

// 基础查询
var user User
err := database.DB(ctx).First(&user, 1).Error

// 事务处理
err := database.Transaction(ctx, func(tx *gorm.DB) error {
    // 事务操作
    return tx.Create(&user).Error
})
```

### dlock - 分布式锁

分布式锁组件，支持 Redis 和 Etcd 后端，内置自动续期。

```go
import "github.com/ceyewan/genesis/pkg/dlock"

// Redis 分布式锁
locker, err := dlock.NewRedis(redisConn, &cfg.DLock,
    dlock.WithLogger(logger),
    dlock.WithTTL(30*time.Second),
)

// 获取锁
if err := locker.Lock(ctx, "resource_key"); err == nil {
    defer locker.Unlock(ctx, "resource_key")

    // 业务逻辑
    logger.Info("lock acquired", clog.String("resource", "resource_key"))
}
```

**主要功能**：

- `Lock(ctx, key)` - 获取锁
- `Unlock(ctx, key)` - 释放锁
- `TryLock(ctx, key)` - 尝试获取锁
- `LockWithTTL(ctx, key, ttl)` - 带TTL的锁

### cache - 缓存组件

统一缓存接口，支持多种后端。

```go
import "github.com/ceyewan/genesis/pkg/cache"

cacheClient, err := cache.New(redisConn, &cfg.Cache,
    cache.WithLogger(logger),
    cache.WithTTL(1*time.Hour),
)

// 设置缓存
err := cacheClient.Set(ctx, "user:123", userData)

// 获取缓存
var result User
found, err := cacheClient.Get(ctx, "user:123", &result)

// 删除缓存
err = cacheClient.Delete(ctx, "user:123")
```

### auth - 身份认证

`auth` 组件提供 JWT（JSON Web Token）身份认证能力，支持 Token 生成、验证和刷新。

```go
import "github.com/ceyewan/genesis/pkg/auth"

// 创建认证器配置
cfg := &auth.Config{
    SecretKey:      "your-secret-key-must-be-at-least-32-chars",
    SigningMethod:  "HS256",
    Issuer:         "my-app",
    AccessTokenTTL: 15 * time.Minute,
    TokenLookup:    "header:Authorization",
    TokenHeadName:  "Bearer",
}

// 创建认证器
authenticator, err := auth.New(cfg, auth.WithLogger(logger))
if err != nil {
    log.Fatal(err)
}

// 生成 Token
claims := auth.NewClaims("user123",
    auth.WithUsername("Alice"),
    auth.WithRoles("user"),
)

token, err := authenticator.GenerateToken(ctx, claims)
if err != nil {
    log.Fatal(err)
}

// 验证 Token
claims, err := authenticator.ValidateToken(ctx, token)
if err != nil {
    log.Fatal(err)
}

// 刷新 Token
newToken, err := authenticator.RefreshToken(ctx, token)
if err != nil {
    log.Fatal(err)
}
```

#### 中间件集成

auth 组件提供了 Gin 框架的中间件支持：

```go
// 使用 Gin 中间件保护路由
router := gin.Default()

// 登录接口
router.POST("/login", func(c *gin.Context) {
    // 验证用户凭据...

    // 生成 Token
    claims := auth.NewClaims(userID,
        auth.WithUsername(username),
        auth.WithRoles("user"),
    )

    token, err := authenticator.GenerateToken(c, claims)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    c.JSON(200, gin.H{"token": token})
})

// 受保护的路由组
protected := router.Group("/api")
protected.Use(authenticator.GinMiddleware())
{
    protected.GET("/profile", func(c *gin.Context) {
        claims := auth.MustGetClaims(c)
        c.JSON(200, gin.H{
            "user_id":  claims.UserID,
            "username": claims.Username,
            "roles":    claims.Roles,
        })
    })
}
```

#### Claims 结构

Claims 支持以下字段：

```go
type Claims struct {
    UserID   string            // 用户唯一标识
    Username string            // 用户名
    Roles    []string          // 用户角色列表
    Custom   map[string]any    // 自定义字段
}
```

#### 配置选项

```go
type Config struct {
    SecretKey      string        // JWT 签名密钥（至少32字符）
    SigningMethod  string        // 签名算法（HS256、HS384、HS512）
    Issuer         string        // Token 签发者
    AccessTokenTTL time.Duration // Token 过期时间
    TokenLookup    string        // Token 查找位置（header:Authorization）
    TokenHeadName  string        // Token 前缀（Bearer）
}
```

#### 高级功能

**自定义 Claims**:

```go
claims := auth.NewClaims("user123",
    auth.WithUsername("Alice"),
    auth.WithRoles("user", "admin"),
    auth.WithCustomField("department", "engineering"),
    auth.WithCustomField("level", 5),
)
```

**Token 刷新**:

```go
// 刷新会生成新的 Token 并继承原 Claims
newToken, err := authenticator.RefreshToken(ctx, oldToken)
```

**错误处理**:

```go
token, err := authenticator.GenerateToken(ctx, claims)
if err != nil {
    // 处理不同类型的错误
    switch {
    case errors.Is(err, auth.ErrInvalidSecretKey):
        // 密钥无效
    case errors.Is(err, auth.ErrInvalidClaims):
        // Claims 无效
    default:
        // 其他错误
    }
}
```

> **详细信息**：参阅 [auth 设计文档](docs/governance/auth-design.md) 和 [示例代码](examples/auth)

## 开发模式

### 标准初始化模式

Genesis 推荐使用 Go 原生的显式依赖注入：

```go
func main() {
    // 1. 配置（最先加载）
    cfg, _ := config.Load("config.yaml")

    // 2. Logger（基础设施）
    logger, _ := clog.New(&cfg.Log)
    defer logger.Flush()

    // 3. 连接器（逆序关闭）
    redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
    defer redisConn.Close()

    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
    defer mysqlConn.Close()

    // 4. 组件（注入依赖）
    cache, _ := cache.New(redisConn, &cfg.Cache, cache.WithLogger(logger))
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock, dlock.WithLogger(logger))

    // 5. 业务服务
    userService := service.NewUserService(cache, locker)

    // 启动应用
    server := api.NewServer(userService)
    server.Run(ctx)
}
```

### 资源管理原则

- **谁创建，谁负责释放**：通过 defer 实现 LIFO 关闭顺序
- **Connector 拥有资源**：负责底层连接的 Close()
- **Component 借用资源**：Close() 通常是 no-op

### 选项模式

所有组件都支持选项模式进行配置：

```go
// 使用选项模式
component, err := pkg.New(conn, config,
    pkg.WithLogger(logger),
    pkg.WithMeter(meter),
    pkg.WithTimeout(30*time.Second),
)
```

## 最佳实践

### 1. 命名空间规范

推荐使用层级命名空间反映应用架构：

```go
// 应用级
appLogger := clog.Default().WithNamespace("user-service")

// 组件级
dbLogger := appLogger.WithNamespace("database")
cacheLogger := appLogger.WithNamespace("cache")

// 操作级
queryLogger := dbLogger.WithNamespace("query")
```

### 2. 错误处理

使用结构化错误字段：

```go
logger.Error("operation failed",
    clog.Error(err),
    clog.String("operation", "createUser"),
    clog.Int("retry_count", 3),
    clog.Duration("elapsed", 2*time.Second))
```

### 3. Context 传播

在 HTTP/gRPC 处理器中传播 Context：

```go
func (h *Handler) HandleRequest(w http.ResponseWriter, r *http.Request) {
    // 从请求中提取字段到 Context
    ctx = context.WithValue(r.Context(), "request_id", getRequestID(r))
    ctx = context.WithValue(ctx, "user_id", getUserID(r))

    // 传递给业务逻辑
    h.userService.Process(ctx, req)
}
```

### 4. 配置管理

将配置集中管理，使用强类型绑定：

```go
type AppConfig struct {
    Log    clog.Config    `yaml:"log"`
    Redis  RedisConfig    `yaml:"redis"`
    MySQL  MySQLConfig    `yaml:"mysql"`
    DLock  dlock.Config   `yaml:"dlock"`
    Cache  cache.Config   `yaml:"cache"`
}

var cfg AppConfig
if err := config.LoadAndUnmarshal("config.yaml", &cfg); err != nil {
    panic(err)
}
```

### 5. 健康检查

利用 Connector 的健康检查能力：

```go
func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    checks := map[string]error{
        "redis": h.redisConn.HealthCheck(ctx),
        "mysql": h.mysqlConn.HealthCheck(ctx),
    }

    // 返回健康状态
    status := "healthy"
    for _, err := range checks {
        if err != nil {
            status = "unhealthy"
            break
        }
    }

    json.NewEncoder(w).Encode(map[string]interface{}{
        "status": status,
        "checks": checks,
    })
}
```

---

## 更多资源

- [设计文档](docs/) - 详细的组件设计文档
- [重构进度](docs/refactoring-progress.md) - 当前重构状态
- [示例代码](examples/) - 各组件的使用示例
- [CLAUDE.md](CLAUDE.md) - 开发规范和指南

## 贡献指南

请参考 [CLAUDE.md](CLAUDE.md) 中的 Git 工作流和提交规范。
