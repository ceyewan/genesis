// pkg/container/container.go
package container

import (
	"context"
	"fmt"
	"time"

	"github.com/ceyewan/genesis/internal/connector"
	"github.com/ceyewan/genesis/internal/connector/manager"
	"github.com/ceyewan/genesis/pkg/clog"
	pkgconnector "github.com/ceyewan/genesis/pkg/connector"
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
}

// Container 应用容器，管理所有组件和连接器
type Container struct {
	// 日志组件
	Log clog.Logger
	// 数据库组件
	DB DB
	// 缓存组件
	Cache Cache
	// 分布式锁组件
	Locker Locker
	// 消息队列组件
	MQ MQ

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
	// 这里可以根据需要初始化各种组件
	// 例如：数据库组件、缓存组件、分布式锁组件等

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

type DB interface{}

type Cache interface{}

type Locker interface{}

type MQ interface{}
