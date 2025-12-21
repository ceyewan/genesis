package connector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type mysqlConnector struct {
	cfg                   *MySQLConfig
	db                    *gorm.DB
	logger                clog.Logger
	meter                 metrics.Meter
	healthy               atomic.Bool
	mu                    sync.RWMutex
	totalConnections      metrics.Counter
	successfulConnections metrics.Counter
	failedConnections     metrics.Counter
	activeConnections     metrics.Gauge
}

// NewMySQL 创建 MySQL 连接器
func NewMySQL(cfg *MySQLConfig, opts ...Option) (MySQLConnector, error) {
	cfg.SetDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid mysql config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}

	c := &mysqlConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "mysql"), clog.String("name", cfg.Name)),
		meter:  opt.meter,
	}

	// 创建简化指标
	if c.meter != nil {
		var err error
		c.totalConnections, err = c.meter.Counter(
			"connector_mysql_total_connections",
			"Total number of MySQL connection attempts",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create total connections counter")
		}

		c.successfulConnections, err = c.meter.Counter(
			"connector_mysql_successful_connections",
			"Number of successful MySQL connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create successful connections counter")
		}

		c.failedConnections, err = c.meter.Counter(
			"connector_mysql_failed_connections",
			"Number of failed MySQL connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create failed connections counter")
		}

		c.activeConnections, err = c.meter.Gauge(
			"connector_mysql_active_connections",
			"Number of active MySQL connections",
		)
		if err != nil {
			return nil, xerrors.Wrapf(err, "create active connections gauge")
		}
	}

	// 构建 DSN
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.Username, cfg.Password, cfg.Host, cfg.Port, cfg.Database, cfg.Charset)

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
	c.mu.Lock()
	defer c.mu.Unlock()

	// 记录总连接尝试
	if c.totalConnections != nil {
		c.totalConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
	}

	c.logger.Info("attempting to connect to mysql",
		clog.String("host", c.cfg.Host),
		clog.Int("port", c.cfg.Port))

	sqlDB, err := c.db.DB()
	if err != nil {
		// 记录失败连接
		if c.failedConnections != nil {
			c.failedConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
		}
		c.logger.Error("failed to get mysql db instance", clog.Error(err))
		return xerrors.Wrapf(err, "mysql connector[%s]: failed to get db instance", c.cfg.Name)
	}

	// 配置连接池
	sqlDB.SetMaxIdleConns(c.cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(c.cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(c.cfg.MaxLifetime)

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		// 记录失败连接
		if c.failedConnections != nil {
			c.failedConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
		}
		c.logger.Error("failed to connect to mysql", clog.Error(err))
		return xerrors.Wrapf(err, "mysql connector[%s]: ping failed", c.cfg.Name)
	}

	// 记录成功连接
	if c.successfulConnections != nil {
		c.successfulConnections.Inc(ctx, metrics.L("connector", c.cfg.Name))
	}
	if c.activeConnections != nil {
		c.activeConnections.Set(ctx, float64(1), metrics.L("connector", c.cfg.Name))
	}

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

	// 减少活跃连接数
	if c.activeConnections != nil {
		c.activeConnections.Set(context.Background(), float64(0), metrics.L("connector", c.cfg.Name))
	}

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
