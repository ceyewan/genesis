// Package dlock 提供了分布式锁组件，支持 Redis 和 Etcd 后端。
//
// dlock 是 Genesis 业务层的核心组件，它在 connector 连接器的基础上提供了：
// - 统一的 Locker 接口，屏蔽不同后端差异
// - 阻塞式加锁（Lock）和非阻塞式尝试加锁（TryLock）
// - 自动续期机制（Watchdog/KeepAlive），保护长期任务
// - 防误删机制，确保只有锁持有者才能释放
// - 与 L0 基础组件（日志、指标）的深度集成
//
// ## 基本使用
//
//	redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
//	defer redisConn.Close()
//
//	locker, _ := dlock.NewRedis(redisConn, &dlock.Config{
//		Prefix:        "myapp:lock:",
//		DefaultTTL:    30 * time.Second,
//		RetryInterval: 100 * time.Millisecond,
//	}, dlock.WithLogger(logger))
//
//	// 阻塞式加锁
//	if err := locker.Lock(ctx, "resource-key"); err != nil {
//		return err
//	}
//	defer locker.Unlock(ctx, "resource-key")
//
//	// 执行临界区代码
//	processResource()
//
// ## 后端切换
//
//	// Redis 后端
//	locker, _ := dlock.NewRedis(redisConn, cfg)
//
//	// Etcd 后端
//	locker, _ := dlock.NewEtcd(etcdConn, cfg)
//
// ## 可观测性
//
// 通过注入 Logger 和 Meter 实现统一的日志和指标收集：
//
//	locker, _ := dlock.NewRedis(conn, cfg,
//		dlock.WithLogger(logger),
//		dlock.WithMeter(meter),
//	)
package dlock

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/connector"
)

// NewRedis 创建 Redis 分布式锁
// 这是标准的工厂函数，支持在不依赖其他容器的情况下独立实例化
//
// 参数:
//   - conn: Redis 连接器
//   - cfg: DLock 配置
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	redisConn, _ := connector.NewRedis(redisConfig)
//	locker, _ := dlock.NewRedis(redisConn, &dlock.Config{
//	    Prefix: "myapp:lock:",
//	    DefaultTTL: 30 * time.Second,
//	}, dlock.WithLogger(logger))
func NewRedis(conn connector.RedisConnector, cfg *Config, opts ...Option) (Locker, error) {
	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger（添加 component 字段）
	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "dlock"))
	}

	return newRedis(conn, cfg, logger, opt.meter)
}

// NewEtcd 创建 Etcd 分布式锁
// 这是标准的工厂函数，支持在不依赖其他容器的情况下独立实例化
//
// 参数:
//   - conn: Etcd 连接器
//   - cfg: DLock 配置
//   - opts: 可选参数 (Logger, Meter)
//
// 使用示例:
//
//	etcdConn, _ := connector.NewEtcd(etcdConfig)
//	locker, _ := dlock.NewEtcd(etcdConn, &dlock.Config{
//	    Prefix: "myapp:lock:",
//	    DefaultTTL: 30 * time.Second,
//	}, dlock.WithLogger(logger))
func NewEtcd(conn connector.EtcdConnector, cfg *Config, opts ...Option) (Locker, error) {
	// 应用选项
	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	// 派生 Logger（添加 component 字段）
	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "dlock"))
	}

	return newEtcd(conn, cfg, logger, opt.meter)
}
