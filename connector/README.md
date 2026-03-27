# connector

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/connector.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/connector)

`connector` 是 Genesis 的 L1 基础设施层组件，负责管理与外部服务的原始连接。它为 Genesis 各上层组件提供统一的初始化、连接建立、健康检查和关闭语义，支持 Redis、MySQL、PostgreSQL、SQLite、Etcd、NATS、Kafka 七种后端。

## 组件定位

- 两阶段初始化：`New` 验证配置，`Connect` 建立 I/O 连接，Fail-fast 且幂等可重试
- 借用模型：连接器拥有底层连接生命周期，上层组件（cache、db、mq、dlock）仅借用，`Close` 是 no-op
- 类型安全：泛型接口 `TypedConnector[T]` 提供编译期类型检查，无运行时类型断言
- 健康检查：`HealthCheck` 主动探测（有 I/O），`IsHealthy` 读取缓存（无 I/O）
- 集成 `clog`，自动注入 `namespace=connector` 与连接器名称，方便按组件过滤日志

`connector` 不负责 ORM 查询、缓存策略、消息路由等业务逻辑，这些能力属于 L2 及以上组件。

## 快速开始

```go
redisConn, err := connector.NewRedis(&connector.RedisConfig{
    Addr: "localhost:6379",
}, connector.WithLogger(logger))
if err != nil {
    return err
}
defer redisConn.Close()

if err := redisConn.Connect(ctx); err != nil {
    return err
}

client := redisConn.GetClient()
client.Set(ctx, "key", "value", time.Hour)
```

## 支持的连接器

| 类型 | 接口 | 底层客户端 | 工厂函数 |
|------|------|------------|----------|
| Redis | `RedisConnector` | `*redis.Client` | `NewRedis` |
| MySQL | `MySQLConnector` | `*gorm.DB` | `NewMySQL` |
| PostgreSQL | `PostgreSQLConnector` | `*gorm.DB` | `NewPostgreSQL` |
| SQLite | `SQLiteConnector` | `*gorm.DB` | `NewSQLite` |
| Etcd | `EtcdConnector` | `*clientv3.Client` | `NewEtcd` |
| NATS | `NATSConnector` | `*nats.Conn` | `NewNATS` |
| Kafka | `KafkaConnector` | `*kgo.Client` | `NewKafka` |

## 推荐使用方式

### 资源所有权

连接器遵循"谁创建，谁释放"原则。上层组件（cache、dlock 等）不应调用 `Close()`，应用层通过 `defer` 按 LIFO 顺序释放：

```go
redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
defer redisConn.Close()
redisConn.Connect(ctx)

// 注入连接器；cache 不拥有其生命周期
dist, _ := cache.NewDistributed(&cfg.Cache, cache.WithRedisConnector(redisConn))
```

### 健康检查

定期调用 `HealthCheck` 更新缓存状态，业务路径用 `IsHealthy` 快速判断：

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

## 错误处理

```go
var (
    ErrConnection  = xerrors.New("connector: connection failed")
    ErrConfig      = xerrors.New("connector: invalid config")
    ErrHealthCheck = xerrors.New("connector: health check failed")
    ErrClientNil   = xerrors.New("connector: client is nil")
)
```

使用 `xerrors.Is` 匹配哨兵错误，`ErrConnection` 可重试，`ErrConfig` 是程序 bug 需修正配置。

## 测试

```bash
go test ./connector/... -count=1
go test -race ./connector/... -count=1
```

集成测试通过 testcontainers 自动启动容器，直接运行即可，无需手动配置 Docker 环境。

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/connector)
- [组件设计博客](../docs/genesis-connector-blog.md)
- [Genesis 文档目录](../docs/README.md)
