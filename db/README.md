# db - Genesis 数据库组件

[![Go Reference](https://pkg.go.dev/badge/github.com/ceyewan/genesis/db.svg)](https://pkg.go.dev/github.com/ceyewan/genesis/db)

`db` 是 Genesis 基础设施层的核心组件，提供基于 GORM 的数据库操作能力，支持分库分表功能。

## 特性

- **所属层级**：L1 (Infrastructure) — 基础设施，封装底层存储访问
- **核心职责**：在 MySQL 连接器的基础上提供 GORM ORM 功能和分库分表能力
- **设计原则**：
  - **借用模型**：借用 MySQL 连接器的连接，不负责连接的生命周期
  - **原生体验**：保持 GORM 的原汁原味，用户主要通过 `*gorm.DB` 进行操作
  - **无侵入分片**：利用 `gorm.io/sharding` 插件实现分库分表，业务代码无需感知分片逻辑
  - **高性能**：基于 SQL 解析和替换，无网络中间件开销
  - **统一事务**：提供简单的闭包接口管理事务生命周期
  - **可观测性**：集成 clog 和 metrics，提供完整的日志和指标能力

## 目录结构（完全扁平化设计）

```text
db/                        # 公开 API + 实现（完全扁平化）
├── README.md              # 本文档
├── db.go                  # DB 接口和实现，New 构造函数
├── config.go              # 配置结构：Config + ShardingRule + SetDefaults/Validate
├── options.go             # 函数式选项：Option、WithLogger/WithMeter
└── *_test.go             # 测试文件
```

**设计原则**：完全扁平化设计，所有公开 API 和实现都在根目录，无 `types/` 子包

## 快速开始

```go
import "github.com/ceyewan/genesis/db"
```

### 基础使用

```go
// 1. 创建连接器
mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
defer mysqlConn.Close()
mysqlConn.Connect(ctx)

// 2. 创建 DB 组件
database, _ := db.New(mysqlConn, &db.Config{
    EnableSharding: true,
    ShardingRules: []db.ShardingRule{
        {
            ShardingKey:    "user_id",
            NumberOfShards: 64,
            Tables:         []string{"orders"},
        },
    },
}, db.WithLogger(logger))

// 3. 使用 GORM 进行数据库操作
gormDB := database.DB(ctx)
var users []User
gormDB.Where("status = ?", "active").Find(&users)

// 4. 事务操作
err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
    return tx.Create(&User{Name: "test"}).Error
})
```

## 核心接口

### DB 接口

```go
type DB interface {
    // DB 获取底层的 *gorm.DB 实例
    // 绝大多数业务查询直接使用此方法返回的对象
    DB(ctx context.Context) *gorm.DB

    // Transaction 执行事务操作
    // fn 中的 tx 对象仅在当前事务范围内有效
    Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error

    // Close 关闭组件
    Close() error
}
```

## 配置设计

### Config 结构

```go
type Config struct {
    // 是否开启分片特性
    EnableSharding bool `json:"enable_sharding" yaml:"enable_sharding"`

    // 分片规则配置列表
    // 允许为不同的表组配置不同的分片规则
    ShardingRules []ShardingRule `json:"sharding_rules" yaml:"sharding_rules"`
}
```

### ShardingRule 结构

```go
type ShardingRule struct {
    // 分片键 (例如 "user_id")
    ShardingKey string `json:"sharding_key" yaml:"sharding_key"`

    // 分片数量 (例如 64)
    NumberOfShards uint `json:"number_of_shards" yaml:"number_of_shards"`

    // 应用此规则的逻辑表名列表 (例如 ["orders", "audit_logs"])
    Tables []string `json:"tables" yaml:"tables"`
}
```

## 分片操作

对于配置了分片的表，操作时**必须包含分片键**：

```go
// 假设 "orders" 表配置了按 "user_id" 分片，共 64 片

// 插入操作：自动路由到 orders_xx 表
// SQL: INSERT INTO orders_02 ... (假设 user_id % 64 = 2)
err := database.DB(ctx).Create(&Order{
    UserID:    12345,
    ProductID: 1001,
    Amount:    99.99,
}).Error

// 查询操作：必须包含 user_id 条件
// SQL: SELECT * FROM orders_02 WHERE user_id = 12345
var orders []Order
err := database.DB(ctx).Where("user_id = ?", 12345).Find(&orders).Error
```

**⚠️ 重要**：如果操作分片表时未提供分片键，将返回错误。

### 普通表操作

对于未配置分片的表，使用方式与标准 GORM 完全一致：

```go
var product Product
err := database.DB(ctx).First(&product, id).Error
```

### 事务操作

事务支持与分片插件无缝集成：

```go
err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
    // 在事务中插入分片表
    if err := tx.Create(order).Error; err != nil {
        return err
    }

    // 在事务中更新普通表
    if err := tx.Create(product).Error; err != nil {
        return err
    }

    return nil
})
```

## 函数式选项

```go
// WithLogger 注入日志记录器
database, err := db.New(mysqlConn, cfg, db.WithLogger(logger))

// WithMeter 注入指标收集器
database, err := db.New(mysqlConn, cfg, db.WithMeter(meter))

// 组合使用
database, err := db.New(mysqlConn, cfg,
    db.WithLogger(logger),
    db.WithMeter(meter))
```

## 配置处理流程

1. `cfg.SetDefaults()` - 设置默认值（自动调用）
2. `cfg.Validate()` - 验证配置有效性（自动调用）
3. 注册分片中间件（如果启用分片）
4. 创建 DB 组件实例

## 资源所有权模型

DB 组件采用**借用模型 (Borrowing Model)**：

1. **连接器 (Owner)**：拥有底层连接，负责创建连接池并在应用退出时执行 `Close()`
2. **DB 组件 (Borrower)**：借用连接器中的客户端，不拥有其生命周期
3. **生命周期控制**：使用 `defer` 确保关闭顺序与创建顺序相反（LIFO）

```go
// ✅ 正确示例
mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
defer mysqlConn.Close() // 应用结束时关闭底层连接

database, _ := db.New(mysqlConn, &cfg.DB, db.WithLogger(logger))
// database.Close() 为 no-op，不需要调用
```

## 与其他组件配合

```go
func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建连接器
    mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
    defer mysqlConn.Close()
    mysqlConn.Connect(ctx)

    // 2. 创建 DB 组件
    database, _ := db.New(mysqlConn, &cfg.DB, db.WithLogger(logger))

    // 3. 使用 DB 组件
    userSvc := service.NewUserService(database)

    // 在业务代码中使用
    user, err := userSvc.GetUser(ctx, userID)
}
```

## 最佳实践

1. **分离创建与连接**：在应用启动阶段先调用 `New` 验证配置（Fail-fast），然后在系统就绪后再连接
2. **必须关闭连接器**：始终使用 `defer mysqlConn.Close()` 释放连接资源
3. **分片键必须提供**：操作分片表时必须包含分片键条件，否则会报错
4. **注入依赖**：务必通过 `WithLogger` 和 `WithMeter` 注入可观测性组件
5. **错误处理**：使用 `xerrors.Wrapf()` 包装错误，保留错误链

## 完整示例

```go
package main

import (
    "context"
    "github.com/ceyewan/genesis/clog"
    "github.com/ceyewan/genesis/connector"
    "github.com/ceyewan/genesis/db"
    "gorm.io/gorm"
)

type User struct {
    ID    uint64 `gorm:"primaryKey"`
    Name  string `gorm:"type:varchar(100)"`
    Email string `gorm:"type:varchar(200)"`
}

type Order struct {
    ID        uint64  `gorm:"primaryKey"`
    UserID    int64   `gorm:"index"` // 分片键
    ProductID int64   `gorm:"index"`
    Amount    float64 `gorm:"type:decimal(10,2)"`
}

func main() {
    ctx := context.Background()
    logger := clog.Must(&clog.Config{Level: "info"})

    // 1. 创建 MySQL 连接器
    mysqlConn, err := connector.NewMySQL(&connector.MySQLConfig{
        Host:     "localhost",
        Port:     3306,
        Username: "root",
        Password: "password",
        Database: "genesis_db",
    }, connector.WithLogger(logger))
    if err != nil {
        panic(err)
    }
    defer mysqlConn.Close()

    // 2. 连接到数据库
    if err := mysqlConn.Connect(ctx); err != nil {
        panic(err)
    }

    // 3. 创建 DB 组件（启用分片）
    database, err := db.New(mysqlConn, &db.Config{
        EnableSharding: true,
        ShardingRules: []db.ShardingRule{
            {
                ShardingKey:    "user_id",
                NumberOfShards: 64,
                Tables:         []string{"orders"},
            },
        },
    }, db.WithLogger(logger))
    if err != nil {
        panic(err)
    }

    // 4. 自动迁移表结构
    gormDB := database.DB(ctx)
    if err := gormDB.AutoMigrate(&User{}, &Order{}); err != nil {
        panic(err)
    }

    // 5. 使用事务创建数据
    err = database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
        // 创建用户
        user := User{Name: "Alice", Email: "alice@example.com"}
        if err := tx.Create(&user).Error; err != nil {
            return err
        }

        // 创建订单（自动路由到分片表）
        order := Order{
            UserID:    int64(user.ID),
            ProductID: 1001,
            Amount:    99.99,
        }
        return tx.Create(&order).Error
    })
    if err != nil {
        panic(err)
    }

    // 6. 查询用户订单（必须包含分片键）
    var orders []Order
    err = database.DB(ctx).Where("user_id = ?", user.ID).Find(&orders).Error
    if err != nil {
        panic(err)
    }

    logger.Info("Database operations completed successfully")
}
```
