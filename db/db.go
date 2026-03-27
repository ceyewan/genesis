// Package db 提供基于 GORM 的数据库组件，是 Genesis L1 基础设施层的一部分。
//
// db 在 connector 提供的连接之上封装 GORM 的初始化、事务管理与可观测性接入。
// 业务代码通过 DB(ctx) 获得 *gorm.DB，继续使用原生 GORM API，
// 同时自动获得 clog SQL 日志与 OpenTelemetry trace 能力。
//
// # 基本用法
//
//	pgConn, _ := connector.NewPostgreSQL(&cfg.PostgreSQL, connector.WithLogger(logger))
//	defer pgConn.Close()
//	pgConn.Connect(ctx)
//
//	database, err := db.New(&db.Config{Driver: "postgresql"},
//		db.WithPostgreSQLConnector(pgConn),
//		db.WithLogger(logger),
//		db.WithTracer(otel.GetTracerProvider()),
//	)
//	if err != nil {
//		return err
//	}
//
//	// GORM 操作
//	gormDB := database.DB(ctx)
//	gormDB.Where("status = ?", "active").Find(&users)
//
//	// 事务
//	err = database.Transaction(ctx, func(ctx context.Context, tx *gorm.DB) error {
//		return tx.Create(&Order{UserID: 1001}).Error
//	})
//
// # 资源所有权
//
// db 采用借用模型：connector 负责连接生命周期，db.Close() 为 no-op。
// 应用层只需 defer connector.Close()，无需管理 db 的生命周期。
//
// # 分表
//
// 分表属于数据库层面的能力，推荐使用数据库原生分区：
//   - PostgreSQL 10+：PARTITION BY HASH / RANGE / LIST
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
