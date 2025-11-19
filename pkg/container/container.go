// pkg/container/container.go
package container

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	"github.com/ceyewan/genesis/internal/connector/manager"
	internaldb "github.com/ceyewan/genesis/internal/db"
	"github.com/ceyewan/genesis/pkg/clog"
	pkgconnector "github.com/ceyewan/genesis/pkg/connector"
	pkgdb "github.com/ceyewan/genesis/pkg/db"
	"github.com/ceyewan/genesis/pkg/dlock"
	dlocktypes "github.com/ceyewan/genesis/pkg/dlock/types"
	"github.com/ceyewan/genesis/pkg/mq"
)

// Config 容器配置
type Config struct {
	// 日志配置
	Log *clog.Config `json:"log" yaml:"log"`
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
	// DLock 组件配置
	DLock *dlock.Config
	// MQ 组件配置
	MQ *mq.Config
}

// Container 应用容器，管理所有组件和连接器
type Container struct {
	// 日志组件
	Log clog.Logger
	// 数据库组件
	DB pkgdb.DB
	// 缓存组件
	Cache Cache
	// 分布式锁组件
	DLock dlock.Locker
	// 消息队列组件
	MQ mq.Client

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

// New 创建新的容器实例
func New(cfg *Config) (*Container, error) {
	c := &Container{
		config:           cfg,
		lifecycleManager: NewLifecycleManager(),
	}

	// 初始化日志
	if err := c.initLogger(); err != nil {
		return nil, fmt.Errorf("初始化日志失败: %w", err)
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
	// 使用clog系统初始化日志
	var logConfig *clog.Config
	if c.config != nil && c.config.Log != nil {
		logConfig = c.config.Log
	} else {
		// 默认配置
		logConfig = &clog.Config{
			Level:       "info",
			Format:      "console",
			Output:      "stdout",
			AddSource:   true,
			EnableColor: false,
		}
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
	if cfg.DB != nil && cfg.MySQL != nil {
		// 获取 MySQL 连接器
		// 注意：这里假设 DB 组件使用主 MySQL 配置
		// 在更复杂的场景中，可能需要专门的 DB 连接配置
		mysqlConnector, err := c.mysqlManager.Get(*cfg.MySQL)
		if err != nil {
			return fmt.Errorf("获取MySQL连接器失败: %w", err)
		}

		database, err := internaldb.New(mysqlConnector, cfg.DB)
		if err != nil {
			return fmt.Errorf("创建数据库组件失败: %w", err)
		}
		c.DB = database
		// 注册到生命周期管理（如果 DB 组件实现了 Lifecycle 接口）
		// 目前 DB 组件没有显式实现 Lifecycle，因为 GORM 连接由 Connector 管理
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

// initMQ 初始化消息队列组件
func (c *Container) initMQ(cfg *Config) error {
	if cfg.MQ == nil {
		return nil
	}

	if cfg.NATS == nil {
		return fmt.Errorf("mq component requires nats config")
	}

	// 获取 NATS 连接器
	natsConn, err := c.GetNATSConnector(*cfg.NATS)
	if err != nil {
		return fmt.Errorf("failed to get nats connector: %w", err)
	}

	// 派生 mq 专用的 Logger
	mqLogger := c.Log.WithNamespace("mq")

	// 创建 MQ 客户端
	client, err := mq.New(natsConn, cfg.MQ, mqLogger)
	if err != nil {
		return fmt.Errorf("failed to create mq client: %w", err)
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

type Cache interface{}
