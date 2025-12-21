# 连接器 (Connector)

`connector` 是 Genesis 基础设施层的核心组件，负责管理与外部服务（MySQL、Redis、Etcd、NATS）的原始连接。它通过封装复杂的连接细节、提供健康检查、生命周期管理以及与 L0 组件（日志、指标、错误）的深度集成，为上层组件提供稳定、类型安全的连接能力。

## 1. 设计原则

- **显式优于隐式**：依赖通过构造函数显式传入，无 DI 容器，避免"魔法"代码
- **简单优于聪明**：采用 Go 原生设计，defer 管理资源生命周期，符合语言习惯
- **接口优先**：提供统一抽象，隐藏实现细节，便于测试和替换
- **资源所有权明确**：谁创建，谁负责释放，避免资源泄露

## 2. 核心特性

- **类型安全**：通过泛型接口 `TypedConnector[T]` 提供原生的客户端访问（如 `*redis.Client`, `*gorm.DB`）
- **资源管理**：严格遵循"谁创建，谁负责释放"的原则，使用 `defer` 自然管理生命周期
- **可观测性**：
    - **日志**：集成 `clog`，自动注入连接器命名空间和上下文
    - **指标**：集成 `metrics`，提供连接状态、重试次数等核心指标
- **错误处理**：使用 `xerrors` 和 Sentinel Errors，提供一致的错误类型和检查能力
- **健康检查**：提供主动探测（`HealthCheck`）和状态缓存（`IsHealthy`）
- **扁平化设计**：接口、配置和实现均在 `connector` 包下，开箱即用

## 3. 快速开始

连接器的推荐使用模式是：**先创建实例，后建立连接**。

```go
package main

import (
    "context"
    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/clog"
)

func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建 Redis 连接器（验证配置并初始化实例）
    conn, err := connector.NewRedis(&connector.RedisConfig{
        Addr: "localhost:6379",
        DB:   0,
    }, connector.WithLogger(logger))
    if err != nil {
        panic(err)
    }

    // 2. 延迟释放资源（必须！）
    defer conn.Close()

    // 3. 建立连接（执行 Ping/Auth 等 I/O 操作）
    if err := conn.Connect(ctx); err != nil {
        panic(err)
    }

    // 4. 获取类型安全的客户端并使用
    client := conn.GetClient()
    client.Set(ctx, "hello", "genesis", 0)
}
```

## 4. 支持的连接器类型

| 类型      | 接口             | 底层客户端         | 工厂函数   |
| :-------- | :--------------- | :----------------- | :--------- |
| **Redis** | `RedisConnector` | `*redis.Client`    | `NewRedis` |
| **MySQL** | `MySQLConnector` | `*gorm.DB`         | `NewMySQL` |
| **Etcd**  | `EtcdConnector`  | `*clientv3.Client` | `NewEtcd`  |
| **NATS**  | `NATSConnector`  | `*nats.Conn`       | `NewNATS`  |

## 5. 资源所有权模型

连接器采用**借用模型 (Borrowing Model)**：

1. **连接器 (Owner)**：拥有底层连接，负责创建连接池并在应用退出时执行 `Close()`。
2. **上层组件 (Borrower)**：如 `cache`, `db`, `mq` 等。它们借用连接器中的客户端，不拥有其生命周期。
3. **生命周期控制**：使用 `defer` 确保关闭顺序与创建顺序相反（LIFO）。

```go
// ✅ 正确示例
redisConn, _ := connector.NewRedis(&cfg.Redis)
defer redisConn.Close() // 应用结束时关闭底层连接

cache, _ := cache.New(redisConn, &cfg.Cache) // 注入连接器，cache.Close() 为 no-op
```

## 6. 错误处理

Connector 使用 Genesis 的 `xerrors` 组件进行统一的错误处理：

### Sentinel Errors

提供专用的哨兵错误，用于错误检查：

```go
var (
    ErrNotConnected  = xerrors.New("connector: not connected")
    ErrAlreadyClosed = xerrors.New("connector: already closed")
    ErrConnection    = xerrors.New("connector: connection failed")
    ErrTimeout       = xerrors.New("connector: timeout")
    ErrConfig        = xerrors.New("connector: invalid config")
    ErrHealthCheck   = xerrors.New("connector: health check failed")
)
```

### 错误处理模式

```go
// 检查特定错误类型
if xerrors.Is(err, connector.ErrConnection) {
    // 处理连接失败
}

// 错误包装示例
return xerrors.Wrapf(err, "redis connector[%s]: connection failed", c.cfg.Name)
```

## 7. 配置结构

### 通用配置

每个连接器都包含一个 `BaseConfig`，用于通用的连接控制：

```go
type BaseConfig struct {
    Name            string        `mapstructure:"name"`              // 连接器名称
    MaxRetries      int           `mapstructure:"max_retries"`       // 最大重试次数
    RetryInterval   time.Duration `mapstructure:"retry_interval"`    // 重试间隔
    ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`   // 连接超时
    HealthCheckFreq time.Duration `mapstructure:"health_check_freq"` // 健康检查频率
}
```

### Redis 配置示例

```go
type RedisConfig struct {
    BaseConfig   `mapstructure:",squash"`  // 继承通用配置
    Addr         string        `mapstructure:"addr"`           // 连接地址，如 "127.0.0.1:6379"
    Password     string        `mapstructure:"password"`       // 认证密码（可选）
    DB           int           `mapstructure:"db"`             // 数据库编号（默认0）
    PoolSize     int           `mapstructure:"pool_size"`      // 连接池大小（默认10）
    MinIdleConns int           `mapstructure:"min_idle_conns"` // 最小空闲连接数（默认5）
    DialTimeout  time.Duration `mapstructure:"dial_timeout"`   // 连接超时（默认5s）
    ReadTimeout  time.Duration `mapstructure:"read_timeout"`   // 读取超时（默认3s）
    WriteTimeout time.Duration `mapstructure:"write_timeout"`  // 写入超时（默认3s）
}
```

### 配置处理流程

1. `cfg.SetDefaults()` - 设置默认值（自动调用）
2. `cfg.Validate()` - 验证配置有效性（自动调用）
3. 创建连接器实例
4. `conn.Connect(ctx)` - 建立实际连接

## 8. 健康检查

可以通过以下方式进行健康检查：

- **主动探测**：调用 `conn.HealthCheck(ctx)`，会向服务器发送 Ping 命令并更新内部状态
- **状态查询**：调用 `conn.IsHealthy()`，返回最近一次探测的结果，无 I/O 开销

```go
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        if err := conn.HealthCheck(ctx); err != nil {
            logger.Warn("connector health check failed", clog.Error(err))
        }
    }
}()
```

## 9. 可观测性集成

### 日志集成

通过 `WithLogger` 注入 `clog.Logger`，自动添加连接器命名空间：

```go
redisConn, err := connector.NewRedis(&cfg.Redis,
    connector.WithLogger(logger))
```

日志输出示例：

```json
{
    "namespace": "connector",
    "connector": "redis",
    "name": "primary",
    "msg": "connected to redis",
    "addr": "127.0.0.1:6379"
}
```

### 指标集成

通过 `WithMeter` 注入 `metrics.Meter`，收集连接池指标：

```go
redisConn, err := connector.NewRedis(&cfg.Redis,
    connector.WithLogger(logger),
    connector.WithMeter(meter))
```

常用指标：

- `connector_redis_total_connections` - 总连接尝试次数
- `connector_redis_successful_connections` - 成功连接次数
- `connector_redis_failed_connections` - 失败连接次数
- `connector_redis_active_connections` - 当前活跃连接数

## 10. 与其他组件配合

连接器为上层组件提供底层连接支持：

```go
func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建连接器
    redisConn, _ := connector.NewRedis(&cfg.Redis,
        connector.WithLogger(logger))
    defer redisConn.Close() // 必须释放资源
    redisConn.Connect(ctx)

    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL,
        connector.WithLogger(logger))
    defer mysqlConn.Close()
    mysqlConn.Connect(ctx)

    // 2. 创建业务组件（注入连接器）
    cache, _ := cache.New(redisConn, &cfg.Cache)
    db, _ := db.New(mysqlConn, &cfg.DB)
    locker, _ := dlock.NewRedis(redisConn, &cfg.DLock)

    // 3. 使用组件
    userSvc := service.NewUserService(db, cache, locker)
}
```

## 11. 最佳实践

1. **分离创建与连接**：在应用启动阶段先调用 `New*` 验证配置（Fail-fast），然后在系统就绪后再调用 `Connect`
2. **必须 Close**：始终使用 `defer conn.Close()` 释放连接资源
3. **单例使用**：在微服务中，每个数据源应只创建一个 Connector 实例并在组件间共享
4. **注入依赖**：务必通过 `WithLogger` 和 `WithMeter` 注入可观测性组件，以便进行线上监控和排障
5. **错误处理**：使用 `xerrors.Is()` 检查特定错误类型，进行精确的错误处理
