package connector

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

type postgresqlConnector struct {
	cfg     *PostgreSQLConfig
	db      *gorm.DB
	logger  clog.Logger
	metrics *connectorTelemetry
	healthy atomic.Bool
	mu      sync.RWMutex
}

// NewPostgreSQL 创建 PostgreSQL 连接器
// 注意：实际连接在调用 Connect() 时建立
func NewPostgreSQL(cfg *PostgreSQLConfig, opts ...Option) (PostgreSQLConnector, error) {
	if cfg == nil {
		return nil, xerrors.Wrap(ErrConfig, "postgresql config is nil")
	}
	cfgCopy := *cfg
	cfg = &cfgCopy
	if err := cfg.validate(); err != nil {
		return nil, xerrors.Wrapf(err, "invalid postgresql config")
	}

	opt := &options{}
	for _, o := range opts {
		o(opt)
	}
	opt.applyDefaults()

	c := &postgresqlConnector{
		cfg:     cfg,
		logger:  opt.logger.With(clog.String("connector", "postgresql"), clog.String("name", cfg.Name)),
		metrics: newConnectorTelemetry(opt.meter, "postgresql", cfg.Name),
	}

	return c, nil
}

// Connect 建立连接
func (c *postgresqlConnector) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 幂等：如果已连接则直接返回
	if c.db != nil {
		return nil
	}

	c.logger.Info("attempting to connect to postgresql",
		clog.String("host", c.cfg.Host),
		clog.Int("port", c.cfg.Port))

	// 构建 DSN：优先使用 cfg.DSN，否则用 URL 格式安全构造（url.UserPassword 会正确编码密码中的特殊字符）
	var dsn string
	if c.cfg.DSN != "" {
		dsn = withPostgresConnectTimeout(c.cfg.DSN, c.cfg.ConnectTimeout)
	} else {
		u := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(c.cfg.Username, c.cfg.Password),
			Host:   fmt.Sprintf("%s:%d", c.cfg.Host, c.cfg.Port),
			Path:   "/" + c.cfg.Database,
		}
		params := url.Values{}
		params.Set("sslmode", c.cfg.SSLMode)
		params.Set("TimeZone", c.cfg.Timezone)
		params.Set("connect_timeout", strconv.FormatInt(max(1, int64(c.cfg.ConnectTimeout.Seconds())), 10))
		u.RawQuery = params.Encode()
		dsn = u.String()
	}

	// 创建 GORM 实例
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		c.logger.Error("failed to open postgresql connection", clog.Error(err))
		return wrapConnectorCause(ErrConnection, err, "postgresql connector[%s]", c.cfg.Name)
	}

	sqlDB, err := db.DB()
	if err != nil {
		c.logger.Error("failed to get postgresql db instance", clog.Error(err))
		return wrapConnectorCause(ErrConnection, err, "postgresql connector[%s]: failed to get db instance", c.cfg.Name)
	}

	// 配置连接池
	sqlDB.SetMaxIdleConns(c.cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(c.cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(c.cfg.ConnMaxLifetime)

	// 测试连接
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		c.logger.Error("failed to connect to postgresql", clog.Error(err))
		return wrapConnectorCause(ErrConnection, err, "postgresql connector[%s]: ping failed", c.cfg.Name)
	}

	c.db = db
	c.healthy.Store(true)
	c.logger.Info("successfully connected to postgresql",
		clog.String("host", c.cfg.Host),
		clog.String("database", c.cfg.Database))

	return nil
}

// Close 关闭连接
func (c *postgresqlConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("closing postgresql connection")
	c.healthy.Store(false)

	if c.db == nil {
		return nil
	}

	// 先置 nil，防止 Close 失败后重复关闭
	db := c.db
	c.db = nil

	sqlDB, err := db.DB()
	if err != nil {
		c.logger.Error("failed to get postgresql db instance for closing", clog.Error(err))
		return err
	}

	if err := sqlDB.Close(); err != nil {
		c.logger.Error("failed to close postgresql connection", clog.Error(err))
		return err
	}

	c.logger.Info("postgresql connection closed successfully")
	return nil
}

// HealthCheck 检查连接健康状态
func (c *postgresqlConnector) HealthCheck(ctx context.Context) error {
	c.mu.RLock()
	db := c.db
	c.mu.RUnlock()

	if db == nil {
		c.healthy.Store(false)
		return c.metrics.observeHealth(ctx, xerrors.Wrapf(ErrClientNil, "postgresql connector[%s]", c.cfg.Name))
	}

	sqlDB, err := db.DB()
	if err != nil {
		c.healthy.Store(false)
		return c.metrics.observeHealth(ctx, wrapConnectorCause(ErrHealthCheck, err, "postgresql connector[%s]", c.cfg.Name))
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		c.healthy.Store(false)
		c.logger.Warn("postgresql health check failed", clog.Error(err))
		return c.metrics.observeHealth(ctx, wrapConnectorCause(ErrHealthCheck, err, "postgresql connector[%s]", c.cfg.Name))
	}

	c.healthy.Store(true)
	return c.metrics.observeHealth(ctx, nil)
}

func withPostgresConnectTimeout(dsn string, timeout time.Duration) string {
	seconds := strconv.FormatInt(max(1, int64(timeout.Seconds())), 10)
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		query := u.Query()
		query.Set("connect_timeout", seconds)
		u.RawQuery = query.Encode()
		return u.String()
	}
	if strings.Contains(strings.ToLower(dsn), "connect_timeout=") {
		return dsn
	}
	return strings.TrimSpace(dsn) + " connect_timeout=" + seconds
}

// IsHealthy 返回缓存的健康状态
func (c *postgresqlConnector) IsHealthy() bool {
	return c.healthy.Load()
}

// Name 返回连接器名称
func (c *postgresqlConnector) Name() string {
	return c.cfg.Name
}

// GetClient 返回 GORM 客户端
func (c *postgresqlConnector) GetClient() *gorm.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.db
}
