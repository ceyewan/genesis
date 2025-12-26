// Package db 提供了基于 GORM 的数据库组件，支持分库分表功能。
//
// db 组件是 Genesis 基础设施层的核心组件，它在 MySQL 连接器的基础上提供了：
// - GORM ORM 功能封装
// - 事务管理支持
// - 分库分表能力（基于 gorm.io/sharding）
// - 与 L0 基础组件（日志、指标、错误）的深度集成
//
// ## 基本使用
//
//	mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
//	defer mysqlConn.Close()
//	mysqlConn.Connect(ctx)
//
//	database, _ := db.New(mysqlConn, &db.Config{
//		EnableSharding: true,
//		ShardingRules: []db.ShardingRule{
//			{
//				ShardingKey:    "user_id",
//				NumberOfShards: 64,
//				Tables:         []string{"orders"},
//			},
//		},
//	}, db.WithLogger(logger))
//
//	// 使用 GORM 进行数据库操作
//	gormDB := database.DB(ctx)
//	var users []User
//	gormDB.Where("status = ?", "active").Find(&users)
//
//	// 事务操作
//	err := database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
//		return tx.Create(&User{Name: "test"}).Error
//	})
//
// ## 分片配置
//
// 分片功能通过配置 ShardingRule 启用：
//
//	type ShardingRule struct {
//		ShardingKey    string   // 分片键，如 "user_id"
//		NumberOfShards uint     // 分片数量
//		Tables         []string // 应用规则的表名列表
//	}
//
// ## 设计原则
//
// - **借用模型**：db 组件借用 MySQL 连接器的连接，不负责连接的生命周期
// - **显式依赖**：通过构造函数显式注入连接器和选项
// - **简单设计**：使用 Go 原生模式，避免复杂的抽象
// - **可观测性**：集成 clog 和 metrics，提供完整的日志和指标能力
package db

import (
	"context"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	"gorm.io/gorm"
	"gorm.io/sharding"
)

// database 是 DB 接口的实现
type database struct {
	client *gorm.DB
	logger clog.Logger
	meter  metrics.Meter
}

// DB 定义了数据库组件的核心能力
type DB interface {
	DB(ctx context.Context) *gorm.DB
	Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error
	Close() error
}

// New 创建数据库组件实例
func New(conn connector.DatabaseConnector, cfg *Config, opts ...Option) (DB, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid config")
	}

	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	if opt.logger == nil {
		opt.logger = clog.Discard()
	}
	if opt.meter == nil {
		opt.meter = metrics.Discard()
	}

	gormDB := conn.GetClient()

	// 配置 GORM logger
	gormDB.Session(&gorm.Session{Logger: newGormLogger(opt.logger)})

	// 注册分片中间件
	if cfg.EnableSharding && len(cfg.ShardingRules) > 0 {
		for _, rule := range cfg.ShardingRules {
			tables := make([]interface{}, len(rule.Tables))
			for i, v := range rule.Tables {
				tables[i] = v
			}

			middleware := sharding.Register(sharding.Config{
				ShardingKey:         rule.ShardingKey,
				NumberOfShards:      rule.NumberOfShards,
				PrimaryKeyGenerator: sharding.PKSnowflake,
			}, tables...)

			if err := gormDB.Use(middleware); err != nil {
				return nil, xerrors.Wrapf(err, "failed to register sharding middleware for tables %v", rule.Tables)
			}
		}
	}

	return &database{
		client: gormDB,
		logger: opt.logger,
		meter:  opt.meter,
	}, nil
}

// DB 获取底层的 *gorm.DB 实例
func (d *database) DB(ctx context.Context) *gorm.DB {
	return d.client.WithContext(ctx)
}

// Transaction 执行事务操作
func (d *database) Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error {
	return d.client.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, tx)
	})
}

// Close 关闭组件
func (d *database) Close() error {
	return nil
}
