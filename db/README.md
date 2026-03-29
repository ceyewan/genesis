# db

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/db.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/db)

`db` 是 Genesis 的 L1 基础设施层组件，在 `connector` 提供的连接之上封装 GORM 的初始化、事务管理与可观测性接入，让业务代码继续使用原生 `*gorm.DB`，同时通过选项接入统一的日志和链路追踪能力。

## 组件定位

- 配置驱动：通过 `Config.Driver` 选择底层实现（mysql / postgresql / sqlite），无需关心 DSN 构造和驱动注册细节
- 借用模型：`db` 借用 `connector` 中的客户端，`Close()` 为 no-op，连接生命周期由 `connector` 负责
- 原生体验：`DB(ctx)` 直接返回 `*gorm.DB`，业务继续使用熟悉的 GORM API，不引入新的查询抽象
- 自动可观测：通过 `WithLogger` 接入 `clog` SQL 日志（支持慢查询标注），注入 `WithTracer` 后生成数据库 span

`db` 不负责 ORM 查询语法封装、分表路由、连接池调参。分表建议使用数据库原生分区能力（PG / MySQL `PARTITION BY`），对应用层完全透明。

## 快速开始

```go
pgConn, err := connector.NewPostgreSQL(&cfg.PostgreSQL, connector.WithLogger(logger))
if err != nil {
    return err
}
defer pgConn.Close()

if err := pgConn.Connect(ctx); err != nil {
    return err
}

database, err := db.New(&db.Config{Driver: "postgresql"},
    db.WithPostgreSQLConnector(pgConn),
    db.WithLogger(logger),
    db.WithTracer(otel.GetTracerProvider()),
)
if err != nil {
    return err
}

// 使用 GORM 操作
gormDB := database.DB(ctx)
var users []User
gormDB.Where("status = ?", "active").Find(&users)

// 事务
err = database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
    return tx.Create(&Order{UserID: 1001, Amount: 99.9}).Error
})
```

## 核心接口

```go
type DB interface {
    DB(ctx context.Context) *gorm.DB
    Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error
    Close() error // no-op，借用模型
}
```

## 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `Driver` | `string` | `"mysql"` | 数据库驱动，支持 `mysql` / `postgresql` / `sqlite` |

## 选项

| 选项 | 说明 |
|------|------|
| `WithLogger(l)` | 注入日志器，SQL 日志自动写入 clog |
| `WithTracer(tp)` | 注入 TracerProvider，自动注册 otelgorm 插件 |
| `WithMySQLConnector(c)` | 注入 MySQL 连接器（Driver="mysql" 时必须） |
| `WithPostgreSQLConnector(c)` | 注入 PostgreSQL 连接器（Driver="postgresql" 时必须） |
| `WithSQLiteConnector(c)` | 注入 SQLite 连接器（Driver="sqlite" 时必须） |
| `WithSilentMode()` | 禁用 SQL 日志，适用于测试环境 |

## 推荐使用方式

### 资源所有权

`connector` 拥有连接生命周期，`db` 只借用，无需调用 `db.Close()`：

```go
mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
defer mysqlConn.Close()

database, _ := db.New(&db.Config{Driver: "mysql"},
    db.WithMySQLConnector(mysqlConn),
    db.WithLogger(logger),
)
// database.Close() 是 no-op，可以不调用
```

### 事务

```go
err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
    if err := tx.Create(&Order{...}).Error; err != nil {
        return err  // 自动回滚
    }
    return tx.Update(...).Error  // 返回 nil 时自动提交
})
```

### SQL 日志

默认输出全部 SQL，慢查询（>200ms）自动标注为 `slow sql`，SQL 错误标注为 `sql error`。测试环境可用 `WithSilentMode()` 关闭。

## 错误

```go
var (
    ErrInvalidConfig               = xerrors.New("db: invalid config")
    ErrMySQLConnectorRequired      = xerrors.New("db: mysql connector is required")
    ErrPostgreSQLConnectorRequired = xerrors.New("db: postgresql connector is required")
    ErrSQLiteConnectorRequired     = xerrors.New("db: sqlite connector is required")
)
```

## 测试

```bash
go test ./db/... -count=1
go test -race ./db/... -count=1
```

集成测试通过 testcontainers 自动启动容器，直接运行即可，无需手动配置 Docker 环境。

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/db)
- [组件设计博客](../docs/genesis-db-blog.md)
- [Genesis 文档目录](../docs/README.md)
