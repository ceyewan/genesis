// pkg/container/container.go
package container

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	"github.com/ceyewan/genesis/internal/connector/manager"
	"github.com/ceyewan/genesis/pkg/cache"
	cachetypes "github.com/ceyewan/genesis/pkg/cache/types"
	"github.com/ceyewan/genesis/pkg/clog"
	pkgconnector "github.com/ceyewan/genesis/pkg/connector"
	pkgdb "github.com/ceyewan/genesis/pkg/db"
	"github.com/ceyewan/genesis/pkg/dlock"
	dlocktypes "github.com/ceyewan/genesis/pkg/dlock/types"
	"github.com/ceyewan/genesis/pkg/mq"
	"github.com/ceyewan/genesis/pkg/telemetry"
	"github.com/ceyewan/genesis/pkg/telemetry/types"
)

// Config 容器配置
type Config struct {
	// MySQL 配置
	MySQL *pkgconnector.MySQLConfig
	// Redis 配置
	Redis *pkgconnector.RedisConfig
	// Etcd 配置
	Etcd *pkgconnector.EtcdConfig
	// NATS 配置
	NATS *pkgconnector.NATSConfig
	// DB 组件配置
	DB *pkgdb.Config
	// Cache 组件配置
	Cache *cachetypes.Config
	// DLock 组件配置
	DLock *dlock.Config
	// MQ 组件配置
	MQ *mq.Config
	// Telemetry 配置
	Telemetry *telemetry.Config
}

// Container 应用容器，管理所有组件和连接器
type Container struct {
	// 日志组件
	Log clog.Logger
	// 数据库组件
	DB pkgdb.DB
	// 缓存组件
	Cache cachetypes.Cache
	// 分布式锁组件
	DLock dlock.Locker
	// 消息队列组件
	MQ mq.Client
	// Telemetry 组件
	Telemetry telemetry.Telemetry
	// Meter 指标接口
	Meter types.Meter
	// Tracer 链路追踪接口
	Tracer types.Tracer

	// 配置
	config *Config

	// 连接器管理器
	mysqlManager *manager.Manager[pkgconnector.MySQLConnector]
	redisManager *manager.Manager[pkgconnector.RedisConnector]
	etcdManager  *manager.Manager[pkgconnector.EtcdConnector]
	natsManager  *manager.Manager[pkgconnector.NATSConnector]

	// 生命周期管理器
	lifecycleManager *LifecycleManager
}

// Option 容器选项函数
type Option func(*Container)

// WithLogger 注入外部 Logger
func WithLogger(logger clog.Logger) Option {
	return func(c *Container) {
		c.Log = logger
	}
}

// WithTracer 注入外部 Tracer
func WithTracer(tracer types.Tracer) Option {
	return func(c *Container) {
		c.Tracer = tracer
	}
}

// WithMeter 注入外部 Meter
func WithMeter(meter types.Meter) Option {
	return func(c *Container) {
		c.Meter = meter
	}
}

// New 创建新的容器实例
// cfg: 容器配置，包含各组件和连接器的配置
// opts: 可选参数，用于注入 Logger、Tracer、Meter 等
func New(cfg *Config, opts ...Option) (*Container, error) {
	c := &Container{
		config:           cfg,
		lifecycleManager: NewLifecycleManager(),
	}

	// 应用选项
	for _, opt := range opts {
		opt(c)
	}

	// 初始化日志 (如果未通过 Option 注入)
	if c.Log == nil {
		if err := c.initLogger(); err != nil {
			return nil, fmt.Errorf("初始化日志失败: %w", err)
		}
	}

	// 初始化 Telemetry (如果配置了且未通过 Option 注入)
	if err := c.initTelemetry(); err != nil {
		return nil, fmt.Errorf("初始化 Telemetry 失败: %w", err)
	}

	// 初始化连接器管理器
	if err := c.initManagers(); err != nil {
		return nil, fmt.Errorf("初始化连接器管理器失败: %w", err)
	}

	// 初始化连接器
	if err := c.initConnectors(cfg); err != nil {
		return nil, fmt.Errorf("初始化连接器失败: %w", err)
	}

	// 初始化组件
	if err := c.initComponents(cfg); err != nil {
		return nil, fmt.Errorf("初始化组件失败: %w", err)
	}

	// 启动所有生命周期对象
	if err := c.lifecycleManager.StartAll(context.Background()); err != nil {
		c.Close()
		return nil, fmt.Errorf("启动生命周期对象失败: %w", err)
	}

	return c, nil
}

// initLogger 初始化日志
func (c *Container) initLogger() error {
	// 使用默认配置创建日志
	logConfig := &clog.Config{
		Level:       "info",
		Format:      "console",
		Output:      "stdout",
		AddSource:   true,
		EnableColor: false,
	}

	logger, err := clog.New(logConfig, &clog.Option{
		NamespaceParts: []string{"container"},
	})
	if err != nil {
		return fmt.Errorf("创建clog日志失败: %w", err)
	}

	c.Log = logger
	return nil
}

// initTelemetry 初始化 Telemetry (如果配置了且未通过 Option 注入)
func (c *Container) initTelemetry() error {
	// 如果已经通过 Option 注入了 Meter 和 Tracer，则跳过
	if c.Meter != nil && c.Tracer != nil {
		return nil
	}

	// 如果没有配置 Telemetry，则跳过
	if c.config.Telemetry == nil {
		return nil
	}

	// 创建 Telemetry 实例
	tel, err := telemetry.New(c.config.Telemetry)
	if err != nil {
		return fmt.Errorf("创建 Telemetry 失败: %w", err)
	}

	c.Telemetry = tel
	c.Meter = tel.Meter()
	c.Tracer = tel.Tracer()

	// 注册 Telemetry 到生命周期管理 (Phase 0，最先启动)
	c.lifecycleManager.Register("telemetry", &telemetryLifecycle{
		tel:   tel,
		phase: PhaseLogger, // Phase 0
	})

	return nil
}

// telemetryLifecycle 包装 Telemetry 以实现 Lifecycle 接口
type telemetryLifecycle struct {
	tel   telemetry.Telemetry
	phase int
}

func (t *telemetryLifecycle) Start(ctx context.Context) error {
	// Telemetry 在 New 时已经初始化，无需额外启动
	return nil
}

func (t *telemetryLifecycle) Stop(ctx context.Context) error {
	if t.tel != nil {
		return t.tel.Shutdown(ctx)
	}
	return nil
}

func (t *telemetryLifecycle) Phase() int {
	return t.phase
}

// initManagers 初始化连接器管理器
func (c *Container) initManagers() error {
	// MySQL 管理器
	c.mysqlManager = manager.NewManager[pkgconnector.MySQLConnector](
		func(config any) (pkgconnector.MySQLConnector, error) {
			cfg, ok := config.(pkgconnector.MySQLConfig)
			if !ok {
				return nil, fmt.Errorf("配置类型不匹配")
			}
			// 使用配置的命名空间，如果没有则使用默认的 "mysql"
			namespace := cfg.LogNamespace
			if namespace == "" {
				namespace = "mysql"
			}
			logger := c.Log.WithNamespace(namespace)
			return connector.NewMySQLConnector("mysql", cfg, logger), nil
		},
		manager.ManagerOptions{
			CheckInterval: 30 * time.Second,
			MaxInstances:  10,
		},
	)

	// Redis 管理器
	c.redisManager = manager.NewManager[pkgconnector.RedisConnector](
		func(config any) (pkgconnector.RedisConnector, error) {
			cfg, ok := config.(pkgconnector.RedisConfig)
			if !ok {
				return nil, fmt.Errorf("配置类型不匹配")
			}
			// 使用配置的命名空间，如果没有则使用默认的 "redis"
			namespace := cfg.LogNamespace
			if namespace == "" {
				namespace = "redis"
			}
			logger := c.Log.WithNamespace(namespace)
			return connector.NewRedisConnector("redis", cfg, logger), nil
		},
		manager.ManagerOptions{
			CheckInterval: 30 * time.Second,
			MaxInstances:  10,
		},
	)

	// Etcd 管理器
	c.etcdManager = manager.NewManager[pkgconnector.EtcdConnector](
		func(config any) (pkgconnector.EtcdConnector, error) {
			cfg, ok := config.(pkgconnector.EtcdConfig)
			if !ok {
				return nil, fmt.Errorf("配置类型不匹配")
			}
			// 使用配置的命名空间，如果没有则使用默认的 "etcd"
			namespace := cfg.LogNamespace
			if namespace == "" {
				namespace = "etcd"
			}
			logger := c.Log.WithNamespace(namespace)
			return connector.NewEtcdConnector("etcd", cfg, logger), nil
		},
		manager.ManagerOptions{
			CheckInterval: 30 * time.Second,
			MaxInstances:  10,
		},
	)

	// NATS 管理器
	c.natsManager = manager.NewManager[pkgconnector.NATSConnector](
		func(config any) (pkgconnector.NATSConnector, error) {
			cfg, ok := config.(pkgconnector.NATSConfig)
			if !ok {
				return nil, fmt.Errorf("配置类型不匹配")
			}
			// 使用配置的命名空间，如果没有则使用默认的 "nats"
			namespace := cfg.LogNamespace
			if namespace == "" {
				namespace = "nats"
			}
			logger := c.Log.WithNamespace(namespace)
			return connector.NewNATSConnector("nats", cfg, logger), nil
		},
		manager.ManagerOptions{
			CheckInterval: 30 * time.Second,
			MaxInstances:  10,
		},
	)

	return nil
}

// initConnectors 初始化连接器
func (c *Container) initConnectors(cfg *Config) error {
	// MySQL 连接器
	if cfg.MySQL != nil {
		mysqlConnector, err := c.mysqlManager.Get(*cfg.MySQL)
		if err != nil {
			return fmt.Errorf("创建MySQL连接器失败: %w", err)
		}
		c.lifecycleManager.Register("mysql", mysqlConnector)
	}

	// Redis 连接器
	if cfg.Redis != nil {
		redisConnector, err := c.redisManager.Get(*cfg.Redis)
		if err != nil {
			return fmt.Errorf("创建Redis连接器失败: %w", err)
		}
		c.lifecycleManager.Register("redis", redisConnector)
	}

	// Etcd 连接器
	if cfg.Etcd != nil {
		etcdConnector, err := c.etcdManager.Get(*cfg.Etcd)
		if err != nil {
			return fmt.Errorf("创建Etcd连接器失败: %w", err)
		}
		c.lifecycleManager.Register("etcd", etcdConnector)
	}

	// NATS 连接器
	if cfg.NATS != nil {
		natsConnector, err := c.natsManager.Get(*cfg.NATS)
		if err != nil {
			return fmt.Errorf("创建NATS连接器失败: %w", err)
		}
		c.lifecycleManager.Register("nats", natsConnector)
	}

	return nil
}

// initComponents 初始化组件
func (c *Container) initComponents(cfg *Config) error {
	// 初始化数据库组件
	if err := c.initDB(cfg); err != nil {
		return fmt.Errorf("初始化数据库组件失败: %w", err)
	}

	// 初始化缓存组件
	if err := c.initCache(cfg); err != nil {
		return fmt.Errorf("初始化缓存组件失败: %w", err)
	}

	// 初始化分布式锁组件
	if err := c.initDLock(cfg); err != nil {
		return fmt.Errorf("初始化分布式锁组件失败: %w", err)
	}

	// 初始化消息队列组件
	if err := c.initMQ(cfg); err != nil {
		return fmt.Errorf("初始化消息队列组件失败: %w", err)
	}

	return nil
}

// initDB 初始化数据库组件
func (c *Container) initDB(cfg *Config) error {
	if cfg.DB == nil {
		return nil
	}

	if cfg.MySQL == nil {
		return fmt.Errorf("db 组件需要 MySQL 配置")
	}

	// 获取 MySQL 连接器
	mysqlConnector, err := c.mysqlManager.Get(*cfg.MySQL)
	if err != nil {
		return fmt.Errorf("获取 MySQL 连接器失败: %w", err)
	}

	// 创建 DB 实例 (使用 Option 模式)
	database, err := pkgdb.New(mysqlConnector, cfg.DB,
		pkgdb.WithLogger(c.Log),
		pkgdb.WithMeter(c.Meter),
		pkgdb.WithTracer(c.Tracer),
	)
	if err != nil {
		return fmt.Errorf("创建数据库组件失败: %w", err)
	}

	c.DB = database
	return nil
}

// initMQ 初始化消息队列组件
func (c *Container) initMQ(cfg *Config) error {
	if cfg.MQ == nil {
		return nil
	}

	if cfg.NATS == nil {
		return fmt.Errorf("mq 组件需要 NATS 配置")
	}

	// 获取 NATS 连接器
	natsConn, err := c.GetNATSConnector(*cfg.NATS)
	if err != nil {
		return fmt.Errorf("获取 NATS 连接器失败: %w", err)
	}

	// 创建 MQ 客户端 (使用 Option 模式)
	client, err := mq.New(natsConn, cfg.MQ,
		mq.WithLogger(c.Log),
		mq.WithMeter(c.Meter),
		mq.WithTracer(c.Tracer),
	)
	if err != nil {
		return fmt.Errorf("创建 MQ 客户端失败: %w", err)
	}

	c.MQ = client
	return nil
}

// initDLock 初始化分布式锁组件
func (c *Container) initDLock(cfg *Config) error {
	if cfg.DLock == nil {
		return nil
	}

	// 派生 dlock 专用的 Logger
	dlockLogger := c.Log.WithNamespace("dlock")

	// TODO: 未来当 DLock 组件支持 WithTracer/WithMeter Option 时，可以这样调用:
	// locker, err := dlock.NewRedis(redisConn, cfg.DLock,
	//     dlock.WithLogger(dlockLogger),
	//     dlock.WithTracer(c.Tracer),
	//     dlock.WithMeter(c.Meter),
	// )

	switch cfg.DLock.Backend {
	case dlocktypes.BackendRedis:
		if cfg.Redis == nil {
			return fmt.Errorf("redis backend requires redis config")
		}
		// 获取 Redis 连接器
		redisConn, err := c.GetRedisConnector(*cfg.Redis)
		if err != nil {
			return fmt.Errorf("failed to get redis connector: %w", err)
		}
		// 创建 dlock
		locker, err := dlock.NewRedis(redisConn, cfg.DLock, dlockLogger)
		if err != nil {
			return fmt.Errorf("failed to create redis locker: %w", err)
		}
		c.DLock = locker
	case dlocktypes.BackendEtcd:
		if cfg.Etcd == nil {
			return fmt.Errorf("etcd backend requires etcd config")
		}
		// 获取 Etcd 连接器
		etcdConn, err := c.GetEtcdConnector(*cfg.Etcd)
		if err != nil {
			return fmt.Errorf("failed to get etcd connector: %w", err)
		}
		// 创建 dlock
		locker, err := dlock.NewEtcd(etcdConn, cfg.DLock, dlockLogger)
		if err != nil {
			return fmt.Errorf("failed to create etcd locker: %w", err)
		}
		c.DLock = locker
	default:
		return fmt.Errorf("unsupported dlock backend: %s", cfg.DLock.Backend)
	}

	return nil
}

// initCache 初始化缓存组件
func (c *Container) initCache(cfg *Config) error {
	if cfg.Cache == nil {
		return nil
	}

	if cfg.Redis == nil {
		return fmt.Errorf("cache 组件需要 Redis 配置")
	}

	// 获取 Redis 连接器
	redisConn, err := c.GetRedisConnector(*cfg.Redis)
	if err != nil {
		return fmt.Errorf("获取 Redis 连接器失败: %w", err)
	}

	// 创建缓存实例，注入 Logger, Meter, Tracer
	cacheInstance, err := cache.New(redisConn, cfg.Cache,
		cache.WithLogger(c.Log),
		cache.WithMeter(c.Meter),
		cache.WithTracer(c.Tracer),
	)
	if err != nil {
		return fmt.Errorf("创建缓存组件失败: %w", err)
	}

	c.Cache = cacheInstance
	return nil
}

// Start 启动容器中所有已注册的生命周期组件
// 按照 Phase 顺序启动 (Phase 越小越先启动)
// 如果注册了 ConfigManager，会在此时启动配置监听
func (c *Container) Start(ctx context.Context) error {
	if c.lifecycleManager != nil {
		return c.lifecycleManager.StartAll(ctx)
	}
	return nil
}

// Close 关闭容器
func (c *Container) Close() error {
	if c.lifecycleManager != nil {
		c.lifecycleManager.StopAll(context.Background())
	}

	// 关闭管理器
	if c.mysqlManager != nil {
		c.mysqlManager.Close()
	}
	if c.redisManager != nil {
		c.redisManager.Close()
	}
	if c.etcdManager != nil {
		c.etcdManager.Close()
	}
	if c.natsManager != nil {
		c.natsManager.Close()
	}

	return nil
}

// GetMySQLConnector 获取MySQL连接器
func (c *Container) GetMySQLConnector(config pkgconnector.MySQLConfig) (pkgconnector.MySQLConnector, error) {
	return c.mysqlManager.Get(config)
}

// GetRedisConnector 获取Redis连接器
func (c *Container) GetRedisConnector(config pkgconnector.RedisConfig) (pkgconnector.RedisConnector, error) {
	return c.redisManager.Get(config)
}

// GetEtcdConnector 获取Etcd连接器
func (c *Container) GetEtcdConnector(config pkgconnector.EtcdConfig) (pkgconnector.EtcdConnector, error) {
	return c.etcdManager.Get(config)
}

// GetNATSConnector 获取NATS连接器
func (c *Container) GetNATSConnector(config pkgconnector.NATSConfig) (pkgconnector.NATSConnector, error) {
	return c.natsManager.Get(config)
}

// RegisterConfigManager 注册配置管理器到容器的生命周期管理
// ConfigManager 应该在 Container 之外创建和加载，然后通过此方法注册
// 这样 Container 会在启动时调用 ConfigManager.Start() 启动配置监听
// 在关闭时调用 ConfigManager.Stop() 停止监听
//
// 使用示例:
//
//	cfgMgr, _ := config.New(...)
//	_ = cfgMgr.Load(ctx)
//	app, _ := container.New(cfg)
//	app.RegisterConfigManager(cfgMgr)
//	_ = app.Start(ctx) // 会自动启动 ConfigManager
func (c *Container) RegisterConfigManager(mgr interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Phase() int
}) {
	c.lifecycleManager.Register("config-manager", mgr)
}
