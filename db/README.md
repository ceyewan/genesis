# db

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/db.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/db)

`db` 是 Genesis 的 L1 基础设施层组件，在 `connector` 提供的连接之上封装 GORM 的初始化、事务管理与可观测性接入，让业务代码继续使用原生 `*gorm.DB`，同时通过选项接入统一的日志和链路追踪能力。

## 组件定位

- 配置驱动：通过 `Config.Driver` 选择底层实现（mysql / postgresql / sqlite），无需关心 DSN 构造和驱动注册细节
- 借用模型：`db` 复用 Connector 的 SQL 池，但拥有独立 GORM 句柄；`Close()` 释放自身语句缓存，不关闭连接池
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
    Close() error // 释放自身语句缓存，不关闭借用池
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
| `WithSlowThreshold(d)` | 设置慢 SQL 阈值；默认 `200ms`，负值非法 |

## 推荐使用方式

### 资源所有权

`connector` 拥有 SQL 连接池，`db` 创建独立的 GORM 实例并只借用该池；`db.Close()` 不关闭连接池。
每个 DB 的 Logger、回调、插件及 Trace 注入彼此隔离。SQLite（包括 `:memory:`）复用原池，不另开数据库。
构造失败也不会关闭借用池。

继承范围限于命名策略、时间函数、事务/迁移/查询设置、方言配置和 SQL clause 列表等行为配置；
Connector 上自行安装的插件和回调不再隐式继承，应在对应组件的 GORM 实例上显式安装，且在并发使用前完成。

```go
// 创建后的 db.DB(ctx) 暴露组件自己的 GORM 句柄。
if err := database.DB(ctx).Use(myPlugin); err != nil {
    return err
}
```

标准接入方式：

```go
mysqlConn, err := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
if err != nil {
    return err
}
defer mysqlConn.Close()
if err := mysqlConn.Connect(ctx); err != nil {
    return err
}

database, err := db.New(&db.Config{Driver: "mysql"},
    db.WithMySQLConnector(mysqlConn),
    db.WithLogger(logger),
)
if err != nil {
    return err
}
// 释放组件自己创建的预编译语句缓存；连接池仍由 mysqlConn 关闭。
defer database.Close()
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

普通 SQL 使用 Debug 级别，默认 Info 日志配置下不输出；慢查询（默认 >200ms）以 Warn 级别标注为 `Slow SQL`，SQL 错误以 Error 级别标注为 `SQL error`。日志会透传请求 Context，支持 trace_id / span_id 关联。使用 `WithSlowThreshold` 可以调整阈值；`WithSilentMode()` 关闭 SQL 日志且跳过 SQL 日志字符串格式化。

## 错误

```go
var (
    ErrInvalidConfig               = xerrors.New("db: invalid config")
    ErrMySQLConnectorRequired      = xerrors.New("db: mysql connector is required")
	    ErrPostgreSQLConnectorRequired = xerrors.New("db: postgresql connector is required")
	    ErrSQLiteConnectorRequired     = xerrors.New("db: sqlite connector is required")
	    ErrConnectorNotReady           = xerrors.New("db: connector is not ready")
	    ErrNilTransaction              = xerrors.New("db: transaction function is nil")
	)
```

注入尚未 `Connect` 或已经关闭的 connector 时，`New` 返回的错误会同时匹配 `db.ErrConnectorNotReady` 和 `connector.ErrClientNil`，调用方可以保留 DB 语义或按统一依赖错误分类。

## 测试

```bash
go test ./db/... -count=1
go test -race ./db/... -count=1
```

集成测试通过 testcontainers 自动启动容器，直接运行即可，无需手动配置 Docker 环境。

## 相关文档

- [包文档](https://pkg.go.dev/github.com/ceyewan/genesis/db)
- [数据库能力边界](../docs/design/dependency-boundaries.md)
- [Genesis 文档目录](../docs/README.md)
