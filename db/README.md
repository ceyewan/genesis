# db - Genesis 数据库组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/db.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/db)

`db` 是 Genesis 基础设施层的核心组件，提供基于 GORM 的数据库操作能力，支持分库分表功能。

## 特性

- **所属层级**：L1 (Infrastructure) — 基础设施，封装底层存储访问
- **核心职责**：在连接器的基础上提供 GORM ORM 功能和分库分表能力
- **支持数据库**：MySQL、PostgreSQL、SQLite
- **设计原则**：
    - **配置驱动**：通过 `Config.Driver` 字段控制底层实现（mysql/postgresql/sqlite）
    - **借用模型**：借用连接器的连接，不负责连接的生命周期
    - **原生体验**：保持 GORM 的原汁原味，用户主要通过 `*gorm.DB` 进行操作
    - **无侵入分片**：利用 `gorm.io/sharding` 插件实现分库分表，业务代码无需感知分片逻辑
    - **高性能**：基于 SQL 解析和替换，无网络中间件开销
    - **GORM 日志集成**：SQL 日志自动输出到 clog，支持静默模式
    - **统一事务**：提供简单的闭包接口管理事务生命周期
    - **OpenTelemetry 集成**：自动为数据库查询创建 trace span

## 目录结构

```text
db/                        # 公开 API + 实现（完全扁平化）
├── README.md              # 本文档
├── db.go                  # DB 接口和实现
├── config.go              # 配置结构：Config + ShardingRule
├── options.go             # 函数式选项：Option、WithLogger/WithTracer/WithConnector
├── gorm_logger.go         # GORM 日志适配器
└── *_test.go             # 测试文件
```

## 快速开始

### MySQL 使用

```go
// 1. 创建连接器
mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
defer mysqlConn.Close()
mysqlConn.Connect(ctx)

// 2. 创建 DB 组件
database, _ := db.New(&db.Config{
    Driver: "mysql",
    EnableSharding: true,
    ShardingRules: []db.ShardingRule{
        {
            ShardingKey:    "user_id",
            NumberOfShards: 64,
            Tables:         []string{"orders"},
        },
    },
},
    db.WithLogger(logger),
    db.WithTracer(otel.GetTracerProvider()),
    db.WithMySQLConnector(mysqlConn),
)

// 3. 使用 GORM 进行数据库操作
gormDB := database.DB(ctx)
var users []User
gormDB.Where("status = ?", "active").Find(&users)
```

### SQLite 使用

```go
// 1. 创建 SQLite 连接器
sqliteConn, _ := connector.NewSQLite(&connector.SQLiteConfig{
    Path: "./app.db",
}, connector.WithLogger(logger))
defer sqliteConn.Close()
sqliteConn.Connect(ctx)

// 2. 创建 DB 组件
database, _ := db.New(&db.Config{
    Driver: "sqlite",
},
    db.WithLogger(logger),
    db.WithTracer(otel.GetTracerProvider()),
    db.WithSQLiteConnector(sqliteConn),
)

// 3. 使用 GORM
gormDB := database.DB(ctx)
gormDB.Create(&User{Name: "Alice"})
```

## 核心接口

### DB 接口

```go
type DB interface {
    DB(ctx context.Context) *gorm.DB
    Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error
    Close() error
}
```

## 配置设计

### Config 结构

```go
type Config struct {
    // Driver 指定数据库驱动类型: "mysql"、"postgresql" 或 "sqlite"
    // 默认值: "mysql"
    Driver string

    // 是否开启分片特性
    EnableSharding bool

    // 分片规则配置列表
    ShardingRules []ShardingRule
}
```

### ShardingRule 结构

```go
type ShardingRule struct {
    ShardingKey    string   // 分片键 (例如 "user_id")
    NumberOfShards uint     // 分片数量 (例如 64)
    Tables         []string // 应用此规则的逻辑表名列表
}
```

## 函数式选项

```go
// WithLogger 注入日志记录器（自动添加 "db" namespace）
database, err := db.New(cfg,
    db.WithLogger(logger),
)

// WithTracer 注入 TracerProvider（用于 OpenTelemetry trace）
database, err := db.New(cfg,
    db.WithTracer(otel.GetTracerProvider()),
)

// WithMySQLConnector 注入 MySQL 连接器
database, err := db.New(cfg,
    db.WithMySQLConnector(mysqlConn),
)

// WithSQLiteConnector 注入 SQLite 连接器
database, err := db.New(cfg,
    db.WithSQLiteConnector(sqliteConn),
)

// WithPostgreSQLConnector 注入 PostgreSQL 连接器
database, err := db.New(cfg,
    db.WithPostgreSQLConnector(pgConn),
)

// WithSilentMode 禁用 SQL 日志输出（适用于测试或不需要日志的场景）
database, err := db.New(cfg,
    db.WithMySQLConnector(mysqlConn),
    db.WithSilentMode(),
)

// 未注入时自动使用 Discard() 实现
```

## OpenTelemetry 集成

db 组件通过 `otelgorm` 插件自动为数据库查询创建 trace span：

```go
import (
    "go.opentelemetry.io/otel"
)

// 注入全局 TracerProvider
database, err := db.New(cfg,
    db.WithTracer(otel.GetTracerProvider()),
    db.WithMySQLConnector(mysqlConn),
)
```

每个数据库操作会自动创建一个 span，包含：
- SQL 语句
- 表名
- 查询耗时
- 错误信息（如有）

## GORM 日志

db 组件自动将 GORM 的 SQL 日志输出到 clog：

- `sql error` — SQL 执行错误
- `slow sql` — 耗时超过 200ms 的慢查询
- `sql` — 普通 SQL 执行日志（debug 级别）

### 禁用 SQL 日志

在测试环境或不需要 SQL 日志的场景，可以使用 `WithSilentMode()` 禁用：

```go
database, err := db.New(&db.Config{Driver: "mysql"},
    db.WithMySQLConnector(conn),
    db.WithSilentMode(), // 禁用 SQL 日志
)
```

## 资源所有权模型

DB 组件采用**借用模型 (Borrowing Model)**：

1. **连接器 (Owner)**：拥有底层连接，负责创建连接池并在应用退出时执行 `Close()`
2. **DB 组件 (Borrower)**：借用连接器中的客户端，不拥有其生命周期

```go
// ✅ 正确示例
mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
defer mysqlConn.Close()  // 应用结束时关闭底层连接

database, _ := db.New(&db.Config{Driver: "mysql"},
    db.WithMySQLConnector(mysqlConn),
    db.WithLogger(logger),
)
// database.Close() 为 no-op，不需要调用
```

## 最佳实践

1. **必须关闭连接器**：始终使用 `defer conn.Close()` 释放连接资源
2. **分片键必须提供**：操作分片表时必须包含分片键条件，否则会报错
3. **测试环境使用 SQLite**：可以在测试环境用 SQLite 验证分片逻辑，生产环境切换到 MySQL 无缝对接
4. **注入依赖**：通过 `WithLogger` 和 `WithTracer` 注入可观测性组件
