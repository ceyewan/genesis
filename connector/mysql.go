package connector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	mysqldrv "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

type mysqlConnector struct {
	cfg     *MySQLConfig
	db      *gorm.DB
	logger  clog.Logger
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewMySQL 创建 MySQL 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewMySQL(cfg *MySQLConfig, opts ...Option) (MySQLConnector, error) {
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

	return c, nil
}

// Connect 建立连接
func (c *mysqlConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.db != nil {
		return nil
	}

	c.logger.Info("attempting to connect to mysql",
		clog.String("host", c.cfg.Host),
		clog.Int("port", c.cfg.Port))

	// 构建 DSN：优先使用 cfg.DSN，否则用驱动 Config 安全构造（避免密码含特殊字符导致解析错误）
	var dsn string
	if c.cfg.DSN != "" {
		dsn = c.cfg.DSN
	} else {
		myCfg := &mysqldrv.Config{
			User:      c.cfg.Username,
			Passwd:    c.cfg.Password,
			Net:       "tcp",
			Addr:      fmt.Sprintf("%s:%d", c.cfg.Host, c.cfg.Port),
			DBName:    c.cfg.Database,
			Params:    map[string]string{"charset": c.cfg.Charset},
			ParseTime: true,
			Loc:       time.Local,
			Timeout:   c.cfg.ConnectTimeout,
		}
		dsn = myCfg.FormatDSN()
	}

	// 创建 GORM 实例
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		c.logger.Error("failed to open mysql connection", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "mysql connector[%s]: %v", c.cfg.Name, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		c.logger.Error("failed to get mysql db instance", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "mysql connector[%s]: failed to get db instance: %v", c.cfg.Name, err)
	}

	// 配置连接池
	sqlDB.SetMaxIdleConns(c.cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(c.cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(c.cfg.ConnMaxLifetime)

	// 测试连接
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		c.logger.Error("failed to connect to mysql", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "mysql connector[%s]: ping failed: %v", c.cfg.Name, err)
	}

	c.db = db
	c.healthy.Store(true)
	c.logger.Info("successfully connected to mysql",
		clog.String("host", c.cfg.Host),
		clog.String("database", c.cfg.Database))

	return nil
}

// Close 关闭连接
func (c *mysqlConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing mysql connection")
	c.healthy.Store(false)

	if c.db == nil {
		return nil
	}

	// 先置 nil，防止 Close 失败后重复关闭
	db := c.db
	c.db = nil

	sqlDB, err := db.DB()
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
	c.mu.RLock()
	db := c.db
	c.mu.RUnlock()

	if db == nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrClientNil, "mysql connector[%s]", c.cfg.Name)
	}

	sqlDB, err := db.DB()
	if err != nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrHealthCheck, "mysql connector[%s]: %v", c.cfg.Name, err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("mysql health check failed", clog.Error(err))
		return xerrors.Wrapf(ErrHealthCheck, "mysql connector[%s]: %v", c.cfg.Name, err)
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db
}
