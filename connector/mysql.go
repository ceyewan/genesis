package connector

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type mysqlConnector struct {
	cfg     *MySQLConfig
	db      *gorm.DB
	logger  clog.Logger
	healthy atomic.Bool
}

// NewMySQL 创建 MySQL 连接器
func NewMySQL(cfg *MySQLConfig, opts ...Option) (MySQLConnector, error) {
	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid mysql config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &mysqlConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "mysql"), clog.String("name", cfg.Name)),
	}

	// 构建 DSN：优先使用 cfg.DSN，否则从各字段拼接
	var dsn string
	if cfg.DSN != "" {
		dsn = cfg.DSN
	} else {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
			cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Charset)
	}

	// 创建 GORM 实例
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, xerrors.Wrapf(err, "mysql connector[%s]: connection failed", cfg.Name)
	}

	c.db = db
	return c, nil
}

// Connect 建立连接
func (c *mysqlConnector) Connect(ctx context.Context) error {
	c.logger.Info("attempting to connect to mysql",
		clog.String("host", c.cfg.Host),
		clog.Int("port", c.cfg.Port))

	sqlDB, err := c.db.DB()
	if err != nil {
		c.logger.Error("failed to get mysql db instance", clog.Error(err))
		return xerrors.Wrapf(err, "mysql connector[%s]: failed to get db instance", c.cfg.Name)
	}

	// 配置连接池
	sqlDB.SetMaxIdleConns(c.cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(c.cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(c.cfg.ConnMaxLifetime)

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		c.logger.Error("failed to connect to mysql", clog.Error(err))
		return xerrors.Wrapf(err, "mysql connector[%s]: ping failed", c.cfg.Name)
	}

	c.healthy.Store(true)
	c.logger.Info("successfully connected to mysql",
		clog.String("host", c.cfg.Host),
		clog.String("database", c.cfg.Database))

	return nil
}

// Close 关闭连接
func (c *mysqlConnector) Close() error {
	c.logger.Info("closing mysql connection")
	c.healthy.Store(false)

	sqlDB, err := c.db.DB()
	if err != nil {
		c.logger.Error("failed to get mysql db instance for closing", clog.Error(err))
		return err
	}

	if err := sqlDB.Close(); err != nil {
		c.logger.Error("failed to close mysql connection", clog.Error(err))
		return err
	}

	c.logger.Info("mysql connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *mysqlConnector) HealthCheck(ctx context.Context) error {
	sqlDB, err := c.db.DB()
	if err != nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(err, "mysql connector[%s]: health check failed - failed to get db instance", c.cfg.Name)
	}

	if err := sqlDB.Ping(); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("mysql health check failed", clog.Error(err))
		return xerrors.Wrapf(err, "mysql connector[%s]: health check failed", c.cfg.Name)
	}

	c.healthy.Store(true)
	return nil
}

// IsHealthy 返回缓存的健康状态
func (c *mysqlConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *mysqlConnector) Name() string {
	return c.cfg.Name
}

// GetClient 返回 GORM 客户端
func (c *mysqlConnector) GetClient() *gorm.DB {
	return c.db
}
