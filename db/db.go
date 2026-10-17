// Package db 提供基于 GORM 的数据库组件，是 Genesis L1 基础设施层的一部分。
//
// db 在 connector 提供的连接之上封装 GORM 的初始化、事务管理与可观测性接入。
// 业务代码通过 DB(ctx) 获得 *gorm.DB，继续使用原生 GORM API，
// 在注入 logger / tracer 时可接入 clog SQL 日志与 OpenTelemetry trace 能力。
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
// db 采用借用模型：connector 拥有 SQL 池，db 拥有独立 GORM 句柄及语句缓存。
// 应用层先注册 connector.Close，再注册 db.Close，保证组件先于借用池清理。
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
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// database 是 DB 接口的实现
type database struct {
	client *gorm.DB
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
	} else {
		config := *cfg
		cfg = &config
	}
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid config")
	}

	opt := options{}
	for _, o := range opts {
		if o != nil {
			o(&opt)
		}
	}
	if opt.slowThreshold < 0 {
		return nil, xerrors.Wrap(ErrInvalidConfig, "slow sql threshold must not be negative")
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
	if gormDB == nil {
		return nil, newConnectorNotReadyError(cfg.Driver)
	}

	// 仅借用 SQL 连接池；每个组件拥有独立的 GORM 配置、回调和插件。
	gormDB, err := openBorrowed(gormDB, newGormLogger(opt.logger, opt.silentMode, opt.slowThreshold))
	if err != nil {
		return nil, xerrors.Wrap(err, "create isolated gorm handle")
	}

	// 添加 OpenTelemetry trace 插件
	if opt.tracer != nil {
		if err := gormDB.Use(otelgorm.NewPlugin(
			otelgorm.WithTracerProvider(opt.tracer),
		)); err != nil {
			return nil, xerrors.Wrap(err, "failed to register otelgorm plugin")
		}
	}

	return &database{client: gormDB}, nil
}

// DB 获取底层的 *gorm.DB 实例
func (d *database) DB(ctx context.Context) *gorm.DB {
	if ctx == nil {
		ctx = context.Background()
	}
	return d.client.WithContext(ctx)
}

// Transaction 执行事务操作
func (d *database) Transaction(ctx context.Context, fn func(ctx context.Context, tx *gorm.DB) error) error {
	if fn == nil {
		return ErrNilTransaction
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return d.client.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(ctx, tx)
	})
}

// Close 关闭组件
// db 只释放自己创建的语句缓存，不关闭借用的 SQL 连接池。
func (d *database) Close() error {
	if statements, ok := d.client.ConnPool.(*gorm.PreparedStmtDB); ok {
		statements.Close()
	}
	return nil
}
