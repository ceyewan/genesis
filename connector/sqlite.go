package connector

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type sqliteConnector struct {
	cfg     *SQLiteConfig
	db      *gorm.DB
	logger  clog.Logger
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewSQLite 创建 SQLite 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewSQLite(cfg *SQLiteConfig, opts ...Option) (SQLiteConnector, error) {
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid sqlite config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &sqliteConnector{
		cfg:    cfg,
		logger: opt.logger.With(clog.String("connector", "sqlite"), clog.String("name", cfg.Name)),
	}

	return c, nil
}

// Connect 建立连接
func (c *sqliteConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.db != nil {
		return nil
	}

	c.logger.Info("attempting to connect to sqlite", clog.String("path", c.cfg.Path))

	db, err := gorm.Open(sqlite.Open(c.cfg.Path), &gorm.Config{})
	if err != nil {
		c.logger.Error("failed to open sqlite", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "sqlite connector[%s]: %v", c.cfg.Name, err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		c.logger.Error("failed to get sqlite db instance", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "sqlite connector[%s]: failed to get db instance: %v", c.cfg.Name, err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		c.logger.Error("failed to ping sqlite", clog.Error(err))
		return xerrors.Wrapf(ErrConnection, "sqlite connector[%s]: ping failed: %v", c.cfg.Name, err)
	}

	c.db = db
	c.healthy.Store(true)
	c.logger.Info("successfully connected to sqlite", clog.String("path", c.cfg.Path))

	return nil
}

// Close 关闭连接
func (c *sqliteConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing sqlite connection")
	c.healthy.Store(false)

	if c.db == nil {
		return nil
	}

	sqlDB, err := c.db.DB()
	if err != nil {
		c.logger.Error("failed to get sqlite db instance for closing", clog.Error(err))
		return err
	}

	if err := sqlDB.Close(); err != nil {
		c.logger.Error("failed to close sqlite connection", clog.Error(err))
		return err
	}

	c.db = nil
	c.logger.Info("sqlite connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *sqliteConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	db := c.db
	c.mu.RUnlock()

	if db == nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrClientNil, "sqlite connector[%s]", c.cfg.Name)
	}

	sqlDB, err := db.DB()
	if err != nil {
		c.healthy.Store(false)
		return xerrors.Wrapf(ErrHealthCheck, "sqlite connector[%s]: %v", c.cfg.Name, err)
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("sqlite health check failed", clog.Error(err))
		return xerrors.Wrapf(ErrHealthCheck, "sqlite connector[%s]: %v", c.cfg.Name, err)
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
	return c.cfg.Name
}

// GetClient 返回 GORM 客户端
func (c *sqliteConnector) GetClient() *gorm.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db
}
