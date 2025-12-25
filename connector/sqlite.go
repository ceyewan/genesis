package connector

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/metrics"
	"github.com/ceyewan/genesis/xerrors"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type sqliteConnector struct {
	path   string
	db     *gorm.DB
	logger clog.Logger
	meter  metrics.Meter
	healthy atomic.Bool
	mu      sync.RWMutex
}

// SQLiteConfig SQLite 配置
type SQLiteConfig struct {
	// 数据库文件路径，如 "./test.db" 或 "file::memory:?cache=shared"
	Path string `json:"path" yaml:"path"`
}

// NewSQLite 创建 SQLite 连接器
func NewSQLite(cfg *SQLiteConfig, opts ...Option) (SQLiteConnector, error) {
	if cfg == nil || cfg.Path == "" {
		return nil, xerrors.New("sqlite path is required")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}

	if opt.logger == nil {
		opt.logger = clog.Discard()
	}

	c := &sqliteConnector{
		path:   cfg.Path,
		logger: opt.logger.With(clog.String("connector", "sqlite"), clog.String("path", cfg.Path)),
		meter:  opt.meter,
	}

	db, err := gorm.Open(sqlite.Open(cfg.Path), &gorm.Config{})
	if err != nil {
		return nil, xerrors.Wrapf(err, "sqlite connector: connection failed")
	}

	c.db = db
	return c, nil
}

// Connect 建立连接
func (c *sqliteConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("connecting to sqlite", clog.String("path", c.path))

	sqlDB, err := c.db.DB()
	if err != nil {
		c.logger.Error("failed to get sqlite db instance", clog.Error(err))
		return xerrors.Wrapf(err, "sqlite connector: failed to get db instance")
	}

	if err := sqlDB.Ping(); err != nil {
		c.logger.Error("failed to connect to sqlite", clog.Error(err))
		return xerrors.Wrapf(err, "sqlite connector: ping failed")
	}

	c.healthy.Store(true)
	c.logger.Info("successfully connected to sqlite")

	return nil
}

// Close 关闭连接
func (c *sqliteConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing sqlite connection")
	c.healthy.Store(false)

	sqlDB, err := c.db.DB()
	if err != nil {
		c.logger.Error("failed to get sqlite db instance for closing", clog.Error(err))
		return err
	}

	if err := sqlDB.Close(); err != nil {
		c.logger.Error("failed to close sqlite connection", clog.Error(err))
		return err
	}

	c.logger.Info("sqlite connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *sqliteConnector) HealthCheck(ctx context.Context) error {
	sqlDB, err := c.db.DB()
	if err != nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(err, "sqlite connector: health check failed")
	}

	if err := sqlDB.Ping(); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("sqlite health check failed", clog.Error(err))
		return err
	}

	c.healthy.Store(true)
	return nil
}

// IsHealthy 返回缓存的健康状态
func (c *sqliteConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *sqliteConnector) Name() string {
	return fmt.Sprintf("sqlite[%s]", c.path)
}

// GetClient 返回 GORM 客户端
func (c *sqliteConnector) GetClient() *gorm.DB {
	return c.db
}
