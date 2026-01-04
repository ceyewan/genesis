// Package dlock 提供了分布式锁组件，支持 Redis 和 Etcd 后端。
//
// dlock 是 Genesis 业务层的核心组件，它在 connector 连接器的基础上提供了：
// - 统一的 Locker 接口，屏蔽不同后端差异
// - 阻塞式加锁（Lock）和非阻塞式尝试加锁（TryLock）
// - 自动续期机制（Redis Watchdog / Etcd Session KeepAlive），保护长期任务
// - 防误删机制，确保只有锁持有者才能释放
// - 与 L0 基础组件的日志深度集成（指标采集为预留扩展）
//
// ## 基本使用
//
//	redisConn, _ := connector.NewRedis(&cfg.Redis, connector.WithLogger(logger))
//	defer redisConn.Close()
//
//	locker, _ := dlock.New(&dlock.Config{
//		Driver:        dlock.DriverRedis,
//		Prefix:        "myapp:lock:",
//		DefaultTTL:    30 * time.Second,
//		RetryInterval: 100 * time.Millisecond,
//	}, dlock.WithRedisConnector(redisConn), dlock.WithLogger(logger))
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
//	locker, _ := dlock.New(&dlock.Config{Driver: dlock.DriverRedis}, dlock.WithRedisConnector(redisConn))
//
//	// Etcd 后端
//	locker, _ := dlock.New(&dlock.Config{Driver: dlock.DriverEtcd}, dlock.WithEtcdConnector(etcdConn))
//
// ## 可观测性
//
// 通过注入 Logger 获取统一日志；Meter 目前仅保留扩展点：
//
//	locker, _ := dlock.New(cfg,
//		dlock.WithRedisConnector(conn),
//		dlock.WithLogger(logger),
//		dlock.WithMeter(meter),
//	)
package dlock

import (
	"github.com/ceyewan/genesis/clog"
	"github.com/ceyewan/genesis/xerrors"
)

// New 创建分布式锁组件（配置驱动）
//
// 通过 cfg.Driver 选择后端，连接器通过 Option 注入：
//   - DriverRedis: WithRedisConnector
//   - DriverEtcd: WithEtcdConnector
func New(cfg *Config, opts ...Option) (Locker, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}

	cfg.setDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	opt := options{}
	for _, o := range opts {
		o(&opt)
	}

	logger := opt.logger
	if logger != nil {
		logger = logger.With(clog.String("component", "dlock"))
	}

	switch cfg.Driver {
	case DriverRedis:
		if opt.redisConnector == nil {
			return nil, xerrors.New("dlock: redis connector is required, use WithRedisConnector")
		}
		return newRedis(opt.redisConnector, cfg, logger, opt.meter)
	case DriverEtcd:
		if opt.etcdConnector == nil {
			return nil, xerrors.New("dlock: etcd connector is required, use WithEtcdConnector")
		}
		return newEtcd(opt.etcdConnector, cfg, logger, opt.meter)
	default:
		return nil, xerrors.New("dlock: unsupported driver: " + string(cfg.Driver))
	}
}
