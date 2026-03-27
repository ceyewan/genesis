// Package db 提供了基于 GORM 的数据库组件。
//
// db 组件是 Genesis 基础设施层的核心组件，它在连接器的基础上提供了：
//   - GORM ORM 功能封装
//   - 事务管理支持
//   - 与 L0 基础组件（日志、链路追踪、错误）的深度集成
//
// ## 基本使用（MySQL）
//
//	mysqlConn, _ := connector.NewMySQL(&cfg.MySQL, connector.WithLogger(logger))
//	defer mysqlConn.Close()
//	mysqlConn.Connect(ctx)
//
//	database, _ := db.New(&db.Config{Driver: "mysql"},
//		db.WithLogger(logger),
//		db.WithTracer(otel.GetTracerProvider()),
//		db.WithMySQLConnector(mysqlConn),
//	)
//
// ## 使用 PostgreSQL
//
//	pgConn, _ := connector.NewPostgreSQL(&cfg.PostgreSQL, connector.WithLogger(logger))
//	defer pgConn.Close()
//	pgConn.Connect(ctx)
//
//	database, _ := db.New(&db.Config{Driver: "postgresql"},
//		db.WithLogger(logger),
//		db.WithTracer(otel.GetTracerProvider()),
//		db.WithPostgreSQLConnector(pgConn),
//	)
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
// ## 设计原则
//
//   - 借用模型：db 组件借用连接器的连接，不负责连接的生命周期
//   - 配置驱动：通过 Config.Driver 字段控制底层实现（mysql/postgresql/sqlite）
//   - 显式依赖：通过构造函数显式注入连接器和选项
//   - 可观测性：集成 clog 和 OpenTelemetry trace，提供完整的日志和链路追踪能力
//
// ## 分表说明
//
// 分表属于数据库层面的能力，推荐使用数据库原生分区功能：
//   - PostgreSQL：PARTITION BY HASH / RANGE / LIST（PG10+）
//   - MySQL：PARTITION BY HASH / RANGE / LIST
//
// 原生分区对应用层完全透明，无需任何应用代码改动。
package db

import (
	"context"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// database 是 DB 接口的实现
type database struct {
	client *gorm.DB
	logger clog.Logger
	tracer trace.Tracer
}

// DB 定义了数据库组件的核心能力
type DB interface {
	DB(ctx context.Context) *gorm.DB
	Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error
	Close() error
}

// New 创建数据库组件实例
func New(cfg *Config, opts ...Option) (DB, error) {
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

	// 根据 Driver 获取对应连接器
	var gormDB *gorm.DB
	switch cfg.Driver {
	case "mysql":
		if opt.mysqlConnector == nil {
			return nil, ErrMySQLConnectorRequired
		}
		gormDB = opt.mysqlConnector.GetClient()
	case "postgresql":
		if opt.postgresqlConnector == nil {
			return nil, ErrPostgreSQLConnectorRequired
		}
		gormDB = opt.postgresqlConnector.GetClient()
	case "sqlite":
		if opt.sqliteConnector == nil {
			return nil, ErrSQLiteConnectorRequired
		}
		gormDB = opt.sqliteConnector.GetClient()
	default:
		return nil, xerrors.Wrapf(ErrInvalidConfig, "unknown driver: %s", cfg.Driver)
	}

	// 配置 GORM logger
	gormDB = gormDB.Session(&gorm.Session{Logger: newGormLogger(opt.logger, opt.silentMode)})

	// 添加 OpenTelemetry trace 插件
	if opt.tracer != nil {
		if err := gormDB.Use(otelgorm.NewPlugin(
			otelgorm.WithTracerProvider(opt.tracer),
		)); err != nil {
			return nil, xerrors.Wrap(err, "failed to register otelgorm plugin")
		}
	}

	// 获取 tracer（用于后续可能的 span 创建）
	var tracer trace.Tracer
	if opt.tracer != nil {
		tracer = opt.tracer.Tracer("github.com/ceyewan/genesis/db")
	}

	return &database{
		client: gormDB,
		logger: opt.logger,
		tracer: tracer,
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
// db 组件采用借用模型，不负责连接的生命周期，因此 Close 为 no-op
func (d *database) Close() error {
	return nil
}
